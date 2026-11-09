// tasks.go 镜像构建／拉取任务管理器。
//
// 0.0.8.0：从全局单任务重构为有界并发任务列表——
// 最多 2 个构建 + 1 个拉取同时进行，超出排队；任务支持停止与删除记录；
// 记录持久化到磁盘，服务重启后未完成任务标记为「已中断」；
// 构建成功覆盖同名镜像时自动清理被替换的悬挂镜像。
package image

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ghoul02015/flowpilot/internal/docker"
)

const (
	maxConcurrentBuilds = 2
	maxConcurrentPulls  = 1
	// maxFinishedTasks 已结束任务记录的保留上限（最新 N 条）。
	maxFinishedTasks = 20
	// minBuildFreeBytes 提交构建任务前的磁盘水位下限（15 GB）。
	minBuildFreeBytes = 15 << 30
	maxTaskLogLines   = maxBuildLogLines
	// doneTaskScanInterval 成功任务自动清理的镜像扫描最小间隔。
	doneTaskScanInterval = 15 * time.Second
)

// 任务状态：queued 排队中｜running 进行中｜done 已完成｜error 失败｜cancelled 已取消｜interrupted 服务重启中断。
const (
	TaskQueued      = "queued"
	TaskRunning     = "running"
	TaskDone        = "done"
	TaskError       = "error"
	TaskCancelled   = "cancelled"
	TaskInterrupted = "interrupted"
)

// 任务类型。
const (
	TaskKindBuild = "build"
	TaskKindPull  = "pull"
)

// TaskAPI 是任务管理器依赖的 Docker 操作子集，便于测试替换。
type TaskAPI interface {
	ImageExists(ctx context.Context, image string) (bool, error)
	PullImage(ctx context.Context, image string, onEvent func(docker.PullEvent)) error
	BuildImage(ctx context.Context, context io.Reader, options docker.BuildOptions, onEvent func(docker.BuildEvent)) error
	Images(ctx context.Context) ([]docker.Image, error)
	ImageID(ctx context.Context, name string) (string, error)
	RemoveImage(ctx context.Context, image string, force bool) error
}

// Task 是一个镜像构建／拉取任务的对外视图。
type Task struct {
	ID        string              `json:"id"`
	Kind      string              `json:"kind"`
	Image     string              `json:"image"`
	Variant   string              `json:"variant,omitempty"`
	State     string              `json:"state"`
	Message   string              `json:"message"`
	Progress  int                 `json:"progress"`
	Summary   []BuildParamSummary `json:"summary"`
	Logs      []string            `json:"logs"`
	CreatedAt time.Time           `json:"createdAt"`
	UpdatedAt time.Time           `json:"updatedAt"`

	cancel context.CancelFunc `json:"-"`
	// stopRequested 处理取消先于 goroutine 注册 cancelFunc 的竞态。
	stopRequested bool        `json:"-"`
	params        BuildParams `json:"-"`
	oldID         string      `json:"-"`
}

// TaskNotFoundError 表示任务不存在。
type TaskNotFoundError struct{ ID string }

func (e *TaskNotFoundError) Error() string { return fmt.Sprintf("任务不存在：%s", e.ID) }

// Manager 有界并发地管理镜像构建／拉取任务。
type Manager struct {
	docker    TaskAPI
	storePath string
	diskFree  func(path string) (uint64, error)

	mu       sync.Mutex
	tasks    []*Task
	seq      int
	lastScan time.Time
}

// NewManager 创建任务管理器；storePath 非空时任务记录持久化到该文件，
// 启动时将上次遗留的排队／进行中任务标记为「已中断」。
func NewManager(dockerClient TaskAPI, storePath string) *Manager {
	manager := &Manager{docker: dockerClient, storePath: storePath, diskFree: statfsFree}
	manager.load()
	return manager
}

// Exists 检查镜像是否在本地。
func (m *Manager) Exists(ctx context.Context, image string) (bool, error) {
	if err := validateImage(image); err != nil {
		return false, err
	}
	return m.docker.ImageExists(ctx, image)
}

// List 返回全部任务（最新在前），并惰性清理已成功入库的任务记录。
func (m *Manager) List() []Task {
	m.pruneDoneWithImage()
	m.mu.Lock()
	defer m.mu.Unlock()
	tasks := make([]Task, 0, len(m.tasks))
	for _, task := range m.tasks {
		tasks = append(tasks, snapshotTask(task))
	}
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].CreatedAt.After(tasks[j].CreatedAt) })
	return tasks
}

// Get 返回单个任务。
func (m *Manager) Get(id string) (Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.findLocked(id)
	if task == nil {
		return Task{}, &TaskNotFoundError{ID: id}
	}
	return snapshotTask(task), nil
}

// SubmitBuild 提交一个镜像构建任务；同名镜像有活动任务时返回冲突，
// 并发生成位满时进入排队；提交前做磁盘水位检查。
func (m *Manager) SubmitBuild(params BuildParams) (Task, error) {
	recipe, err := params.validate()
	if err != nil {
		return Task{}, err
	}
	if err := m.checkDiskSpace(); err != nil {
		return Task{}, err
	}
	contextReader, err := BuildContext(params, recipe)
	if err != nil {
		return Task{}, err
	}
	// 构建上下文很小（单个 Dockerfile），序列化保存供出队时使用。
	contextBytes, err := io.ReadAll(contextReader)
	if err != nil {
		return Task{}, fmt.Errorf("生成构建上下文失败：%w", err)
	}

	m.mu.Lock()
	if active := m.activeByImageLocked(params.ImageName); active != nil {
		m.mu.Unlock()
		return Task{}, &ConflictError{Image: params.ImageName}
	}
	m.seq++
	task := &Task{
		ID:        fmt.Sprintf("img-%d-%d", time.Now().Unix(), m.seq),
		Kind:      TaskKindBuild,
		Image:     params.ImageName,
		Variant:   recipe.ID,
		State:     TaskQueued,
		Message:   "排队中，等待构建槽位",
		Summary:   buildSummary(params, recipe),
		Logs:      []string{"已提交构建 " + params.ImageName + " · " + recipe.Label},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		params:    params,
	}
	m.tasks = append(m.tasks, task)
	shouldRun := m.countRunningLocked(TaskKindBuild) < maxConcurrentBuilds
	if shouldRun {
		task.State = TaskRunning
		task.Message = "正在准备 Docker 构建上下文"
		task.Progress = 1
	}
	m.mu.Unlock()
	m.save()

	if shouldRun {
		go m.runBuild(task, contextBytes)
	}
	return snapshotTask(task), nil
}

// SubmitPull 提交一个镜像拉取任务；同名镜像有活动任务时返回冲突。
func (m *Manager) SubmitPull(image string) (Task, error) {
	image = strings.TrimSpace(image)
	if err := validateImage(image); err != nil {
		return Task{}, err
	}
	m.mu.Lock()
	if active := m.activeByImageLocked(image); active != nil {
		m.mu.Unlock()
		return Task{}, &ConflictError{Image: image}
	}
	m.seq++
	now := time.Now()
	task := &Task{
		ID:        fmt.Sprintf("img-%d-%d", now.Unix(), m.seq),
		Kind:      TaskKindPull,
		Image:     image,
		State:     TaskQueued,
		Message:   "排队中，等待拉取槽位",
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.tasks = append(m.tasks, task)
	shouldRun := m.countRunningLocked(TaskKindPull) < maxConcurrentPulls
	if shouldRun {
		task.State = TaskRunning
		task.Message = "正在连接镜像仓库"
	}
	m.mu.Unlock()
	m.save()

	if shouldRun {
		go m.runPull(task)
	}
	return snapshotTask(task), nil
}

// Cancel 停止任务：排队中的任务直接取消，进行中的任务通过取消 context 中止。
func (m *Manager) Cancel(id string) (Task, error) {
	m.mu.Lock()
	task := m.findLocked(id)
	if task == nil {
		m.mu.Unlock()
		return Task{}, &TaskNotFoundError{ID: id}
	}
	switch task.State {
	case TaskQueued:
		task.State = TaskCancelled
		task.Message = "已在排队中取消"
		task.UpdatedAt = time.Now()
		m.mu.Unlock()
		m.save()
		return snapshotTask(task), nil
	case TaskRunning:
		cancel := task.cancel
		task.stopRequested = true
		task.Message = "正在停止…"
		task.UpdatedAt = time.Now()
		m.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		m.save()
		return snapshotTask(task), nil
	default:
		m.mu.Unlock()
		return Task{}, &ValidationError{Message: "任务已结束，无需停止"}
	}
}

// Delete 删除已结束的任务记录；进行中／排队中的任务需先停止。
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for index, task := range m.tasks {
		if task.ID != id {
			continue
		}
		if task.State == TaskQueued || task.State == TaskRunning {
			return &ValidationError{Message: "任务仍在进行中，请先停止再删除记录"}
		}
		m.tasks = append(m.tasks[:index], m.tasks[index+1:]...)
		m.saveLocked()
		return nil
	}
	return &TaskNotFoundError{ID: id}
}

func (m *Manager) findLocked(id string) *Task {
	for _, task := range m.tasks {
		if task.ID == id {
			return task
		}
	}
	return nil
}

func (m *Manager) countRunningLocked(kind string) int {
	count := 0
	for _, task := range m.tasks {
		if task.Kind == kind && task.State == TaskRunning {
			count++
		}
	}
	return count
}

// activeByImageLocked 返回同名镜像的排队／进行中任务（不限类型）。
func (m *Manager) activeByImageLocked(image string) *Task {
	for _, task := range m.tasks {
		if normalizeImageTag(task.Image) == normalizeImageTag(image) && (task.State == TaskQueued || task.State == TaskRunning) {
			return task
		}
	}
	return nil
}

// checkDiskSpace 构建前磁盘水位检查；采集失败不阻塞提交。
func (m *Manager) checkDiskSpace() error {
	if m.diskFree == nil {
		return nil
	}
	path := "."
	if m.storePath != "" {
		path = filepath.Dir(m.storePath)
	}
	free, err := m.diskFree(path)
	if err != nil {
		return nil
	}
	if free < minBuildFreeBytes {
		return &ValidationError{Message: fmt.Sprintf("磁盘剩余空间不足 %.0f GB（当前可用 %.1f GB），请先清理后再构建", float64(minBuildFreeBytes)/(1<<30), float64(free)/(1<<30))}
	}
	return nil
}

func (m *Manager) runBuild(task *Task, contextBytes []byte) {
	// 记录被覆盖的旧镜像 ID，成功后用于清理悬挂镜像。
	if oldID, err := m.docker.ImageID(context.Background(), task.Image); err == nil {
		task.oldID = oldID
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	task.cancel = cancel
	stopRequested := task.stopRequested
	m.mu.Unlock()
	if stopRequested {
		cancel()
	}

	recipe, _ := RecipeByID(task.Variant)
	options := docker.BuildOptions{
		Tag:         task.Image,
		Dockerfile:  "Dockerfile",
		NetworkMode: "host",
		Platform:    dockerPlatform(task.params.Platform),
		BuildArgs: map[string]string{
			"PIP_INDEX_URL":   task.params.PipIndexURL,
			"TORCH_INDEX_URL": torchIndexURL(recipe),
			"COMFYUI_REF":     task.params.ComfyUIRef,
			"WITH_MANAGER":    task.params.WithManager,
		},
	}
	err := m.docker.BuildImage(ctx, bytes.NewReader(contextBytes), options, func(event docker.BuildEvent) {
		message := strings.TrimSpace(event.Message())
		if message != "" {
			m.appendLog(task, message, inferBuildProgress(message))
		}
	})
	m.finish(task, err, "镜像构建完成")
	if err == nil {
		m.cleanupReplacedImage(task)
	}
}

func (m *Manager) runPull(task *Task) {
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	task.cancel = cancel
	stopRequested := task.stopRequested
	m.mu.Unlock()
	if stopRequested {
		cancel()
	}

	err := m.docker.PullImage(ctx, task.Image, func(event docker.PullEvent) {
		message := event.Status
		if event.Progress != "" {
			message = event.Status + " " + event.Progress
		}
		m.appendLog(task, message, 0)
	})
	m.finish(task, err, "镜像拉取完成")
}

// finish 收口任务终态并调度排队任务。
func (m *Manager) finish(task *Task, err error, successMessage string) {
	m.mu.Lock()
	switch {
	case err == nil:
		task.State = TaskDone
		task.Progress = 100
		task.Message = successMessage
	case errors.Is(err, context.Canceled):
		task.State = TaskCancelled
		task.Message = "已手动停止"
	default:
		task.State = TaskError
		task.Message = err.Error()
	}
	task.UpdatedAt = time.Now()
	task.cancel = nil
	task.Logs = append(task.Logs, task.Message)
	if len(task.Logs) > maxTaskLogLines {
		task.Logs = append([]string(nil), task.Logs[len(task.Logs)-maxTaskLogLines:]...)
	}
	m.mu.Unlock()
	m.save()
	m.pumpQueue(task.Kind)
	m.trimFinished()
}

// pumpQueue 同类型任务有完成时，按提交顺序启动下一个排队任务。
func (m *Manager) pumpQueue(kind string) {
	m.mu.Lock()
	var next *Task
	if m.countRunningLocked(kind) < concurrencyLimit(kind) {
		for _, candidate := range m.tasks {
			if candidate.Kind == kind && candidate.State == TaskQueued {
				next = candidate
				break
			}
		}
		if next != nil {
			next.State = TaskRunning
			next.UpdatedAt = time.Now()
			if kind == TaskKindBuild {
				next.Message = "正在准备 Docker 构建上下文"
				next.Progress = 1
			} else {
				next.Message = "正在连接镜像仓库"
			}
		}
	}
	m.mu.Unlock()
	if next == nil {
		return
	}
	if kind == TaskKindBuild {
		recipe, ok := RecipeByID(next.Variant)
		if !ok {
			m.finish(next, fmt.Errorf("构建配方不存在：%s", next.Variant), "")
			return
		}
		contextReader, err := BuildContext(next.params, recipe)
		if err != nil {
			m.finish(next, err, "")
			return
		}
		contextBytes, err := io.ReadAll(contextReader)
		if err != nil {
			m.finish(next, err, "")
			return
		}
		m.save()
		go m.runBuild(next, contextBytes)
		return
	}
	m.save()
	go m.runPull(next)
}

func concurrencyLimit(kind string) int {
	if kind == TaskKindPull {
		return maxConcurrentPulls
	}
	return maxConcurrentBuilds
}

func (m *Manager) appendLog(task *Task, line string, progress int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task.Message = line
	if progress > task.Progress {
		task.Progress = progress
	}
	task.UpdatedAt = time.Now()
	task.Logs = append(task.Logs, line)
	if len(task.Logs) > maxTaskLogLines {
		task.Logs = append([]string(nil), task.Logs[len(task.Logs)-maxTaskLogLines:]...)
	}
}

// cleanupReplacedImage 构建成功且覆盖了同名旧镜像时，删除变成悬挂的旧镜像。
func (m *Manager) cleanupReplacedImage(task *Task) {
	if task.oldID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	images, err := m.docker.Images(ctx)
	if err != nil {
		return
	}
	for _, image := range images {
		if image.ID != task.oldID {
			continue
		}
		if hasValidTag(image.RepoTags) {
			return
		}
		if err := m.docker.RemoveImage(ctx, image.ID, false); err == nil {
			m.appendLog(task, "已自动清理被替换的旧镜像 "+shortImageID(image.ID), 0)
			m.save()
		}
		return
	}
}

// pruneDoneWithImage 惰性清理镜像已入库的成功任务记录（扫描间隔 15 秒）。
func (m *Manager) pruneDoneWithImage() {
	m.mu.Lock()
	if time.Since(m.lastScan) < doneTaskScanInterval {
		m.mu.Unlock()
		return
	}
	m.lastScan = time.Now()
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	images, err := m.docker.Images(ctx)
	cancel()
	if err != nil {
		return
	}
	tags := map[string]bool{}
	for _, image := range images {
		for _, tag := range image.RepoTags {
			tags[tag] = true
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.tasks[:0]
	changed := false
	for _, task := range m.tasks {
		if task.State == TaskDone && tags[normalizeImageTag(task.Image)] {
			changed = true
			continue
		}
		kept = append(kept, task)
	}
	m.tasks = kept
	if changed {
		m.saveLocked()
	}
}

// trimFinished 已结束任务记录仅保留最新 maxFinishedTasks 条。
func (m *Manager) trimFinished() {
	m.mu.Lock()
	defer m.mu.Unlock()
	finished := 0
	for _, task := range m.tasks {
		if isFinished(task.State) {
			finished++
		}
	}
	if finished <= maxFinishedTasks {
		return
	}
	excess := finished - maxFinishedTasks
	kept := m.tasks[:0]
	for _, task := range m.tasks {
		if isFinished(task.State) && excess > 0 {
			excess--
			continue
		}
		kept = append(kept, task)
	}
	m.tasks = kept
	m.saveLocked()
}

func isFinished(state string) bool {
	return state == TaskDone || state == TaskError || state == TaskCancelled || state == TaskInterrupted
}

// ---- 持久化 ----

func (m *Manager) load() {
	if m.storePath == "" {
		return
	}
	data, err := os.ReadFile(m.storePath)
	if err != nil {
		return
	}
	var stored []Task
	if err := json.Unmarshal(data, &stored); err != nil {
		return
	}
	changed := false
	for index := range stored {
		task := stored[index]
		if task.State == TaskQueued || task.State == TaskRunning {
			task.State = TaskInterrupted
			task.Message = "服务重启，任务已中断"
			task.UpdatedAt = time.Now()
			changed = true
		}
		m.tasks = append(m.tasks, &task)
	}
	if changed {
		m.saveLocked()
	}
}

func (m *Manager) save() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveLocked()
}

func (m *Manager) saveLocked() {
	if m.storePath == "" {
		return
	}
	tasks := make([]Task, 0, len(m.tasks))
	for _, task := range m.tasks {
		tasks = append(tasks, snapshotTask(task))
	}
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.storePath), 0o755); err != nil {
		return
	}
	tmp := m.storePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, m.storePath)
}

func snapshotTask(task *Task) Task {
	snapshot := *task
	snapshot.cancel = nil
	snapshot.Summary = append([]BuildParamSummary{}, task.Summary...)
	snapshot.Logs = append([]string{}, task.Logs...)
	return snapshot
}

// normalizeImageTag 为未显式带 tag 的镜像名补 latest，便于与镜像清单比对。
func normalizeImageTag(image string) string {
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon > lastSlash {
		return image
	}
	return image + ":latest"
}

func hasValidTag(tags []string) bool {
	for _, tag := range tags {
		if tag != "" && tag != "<none>:<none>" {
			return true
		}
	}
	return false
}

func shortImageID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// statfsFree 返回路径所在文件系统的可用字节数。
func statfsFree(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}
