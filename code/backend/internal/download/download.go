// Package download 实现模型 URL 下载任务引擎：断点续传、暂停继续、
// 可选 SHA256 校验，完成后写入共享模型仓库。
//
// 任务保存在内存中，平台重启后任务列表清空（仓库文件不受影响）。
package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	stagingDirName = ".downloading"
	copyBufferSize = 64 << 10
)

var (
	categoryPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	sha256Pattern   = regexp.MustCompile(`(?i)^[0-9a-f]{64}$`)
)

// Task 是下载任务的对外视图。
type Task struct {
	ID              string    `json:"id"`
	Kind            string    `json:"kind"` // download | import
	URL             string    `json:"url,omitempty"`
	Source          string    `json:"source,omitempty"` // import 的源文件路径
	Category        string    `json:"category"`
	FileName        string    `json:"fileName"`
	Path            string    `json:"path"`       // 目标相对路径（仓库内）
	Status          string    `json:"status"`     // running | paused | completed | failed | cancelled
	TotalBytes      int64     `json:"totalBytes"` // -1 表示未知
	DownloadedBytes int64     `json:"downloadedBytes"`
	SHA256          string    `json:"sha256,omitempty"`
	Message         string    `json:"message"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// Spec 是创建下载任务的请求参数。
type Spec struct {
	URL      string `json:"url"`
	Category string `json:"category"`
	Name     string `json:"name"`
	SHA256   string `json:"sha256"`
}

// ImportSpec 是本地导入任务的请求参数。
type ImportSpec struct {
	Path     string `json:"path"` // 服务器文件绝对路径，必须位于允许的导入目录内
	Category string `json:"category"`
	Name     string `json:"name"`
	SHA256   string `json:"sha256"`
}

// ValidationError 表示请求参数不合法。
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

// NotFoundError 表示任务不存在。
type NotFoundError struct{ ID string }

func (e *NotFoundError) Error() string { return fmt.Sprintf("下载任务不存在：%s", e.ID) }

// ConflictError 表示任务状态不允许当前操作。
type ConflictError struct{ Message string }

func (e *ConflictError) Error() string { return e.Message }

type taskState struct {
	task       Task
	cancel     context.CancelFunc
	stopReason string // pause／cancel 触发的期望终态
	running    bool
}

// Manager 管理下载任务生命周期。
type Manager struct {
	root       string
	importDirs []string
	storePath  string // 任务持久化文件；空表示不持久化
	client     *http.Client
	mu         sync.Mutex
	tasks      map[string]*taskState
	now        func() time.Time
	idSeq      int64
}

// NewManager 创建下载任务管理器，root 为模型仓库根目录，
// importDirs 为允许本地导入的目录前缀（容器内路径），
// storePath 为任务持久化文件路径（空则仅保存在内存）。
// 持久化文件存在时自动恢复任务：重启前进行中的任务转为已暂停，
// 分片文件仍在 .downloading/ 中，可继续或重试。
func NewManager(root string, importDirs []string, storePath string) *Manager {
	manager := &Manager{
		root:       root,
		importDirs: importDirs,
		storePath:  storePath,
		// 不设整体超时（下载可能持续很久，由 ctx 控制），但对连接建立、
		// TLS 握手与响应头设置超时，避免对端挂起时任务永远卡在建连阶段。
		client: &http.Client{Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			ExpectContinueTimeout: 15 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		}},
		tasks: make(map[string]*taskState),
		now:   time.Now,
	}
	manager.load()
	return manager
}

// persistedTasks 是任务持久化文件的顶层结构。
type persistedTasks struct {
	Tasks []Task `json:"tasks"`
	IDSeq int64  `json:"idSeq"`
}

// load 从持久化文件恢复任务；文件缺失或损坏时静默忽略（任务记录非关键数据）。
// 重启前处于 running 的任务协程已不存在，转为 paused 等待用户继续。
func (m *Manager) load() {
	if m.storePath == "" {
		return
	}
	data, err := os.ReadFile(m.storePath)
	if err != nil {
		return
	}
	var persisted persistedTasks
	if err := json.Unmarshal(data, &persisted); err != nil {
		return
	}
	for _, task := range persisted.Tasks {
		if task.ID == "" {
			continue
		}
		if task.Status == "running" {
			task.Status = "paused"
			if task.Kind == "import" {
				task.Message = "平台重启，导入已暂停，可继续"
			} else {
				task.Message = "平台重启，下载已暂停，可断点续传"
			}
		}
		m.tasks[task.ID] = &taskState{task: task}
	}
	m.idSeq = persisted.IDSeq
}

// persist 将任务快照原子写入持久化文件；调用方须持有 m.mu。
func (m *Manager) persist() {
	if m.storePath == "" {
		return
	}
	snapshot := persistedTasks{Tasks: make([]Task, 0, len(m.tasks)), IDSeq: m.idSeq}
	for _, state := range m.tasks {
		snapshot.Tasks = append(snapshot.Tasks, state.task)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	temp := m.storePath + ".tmp"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(temp, m.storePath)
}

// List 返回全部任务，按创建时间倒序。
func (m *Manager) List() []Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	tasks := make([]Task, 0, len(m.tasks))
	for _, state := range m.tasks {
		tasks = append(tasks, state.task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.After(tasks[j].CreatedAt) })
	return tasks
}

// Get 返回单个任务。
func (m *Manager) Get(id string) (Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.tasks[id]
	if !ok {
		return Task{}, &NotFoundError{ID: id}
	}
	return state.task, nil
}

// Create 校验参数并启动下载任务。
func (m *Manager) Create(spec Spec) (Task, error) {
	task, err := m.buildTask(spec)
	if err != nil {
		return Task{}, err
	}
	return m.register(task)
}

// CreateImport 校验参数并启动本地导入任务。
func (m *Manager) CreateImport(spec ImportSpec) (Task, error) {
	source := filepath.Clean(strings.TrimSpace(spec.Path))
	if !filepath.IsAbs(source) {
		return Task{}, &ValidationError{Message: "导入路径必须是绝对路径"}
	}
	allowed := false
	for _, dir := range m.importDirs {
		cleanDir := filepath.Clean(dir)
		if source == cleanDir || strings.HasPrefix(source, cleanDir+string(filepath.Separator)) {
			allowed = true
			break
		}
	}
	if !allowed {
		return Task{}, &ValidationError{Message: fmt.Sprintf("仅允许从以下目录导入：%s", strings.Join(m.importDirs, "、"))}
	}
	info, err := os.Stat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return Task{}, &NotFoundError{ID: source}
		}
		return Task{}, fmt.Errorf("读取导入文件失败：%w", err)
	}
	if !info.Mode().IsRegular() {
		return Task{}, &ValidationError{Message: "导入源必须是普通文件"}
	}

	name := strings.TrimSpace(spec.Name)
	if name == "" {
		name = filepath.Base(source)
	}
	relPath, fileName, sum, err := m.buildTarget(spec.Category, name, spec.SHA256)
	if err != nil {
		return Task{}, err
	}
	return m.register(Task{
		Kind:       "import",
		Source:     source,
		Category:   categoryOf(relPath),
		FileName:   fileName,
		Path:       relPath,
		Status:     "running",
		TotalBytes: info.Size(),
		SHA256:     sum,
		Message:    "正在复制文件",
		CreatedAt:  m.now(),
		UpdatedAt:  m.now(),
	})
}

// register 登记任务并启动协程。
func (m *Manager) register(task Task) (Task, error) {
	m.mu.Lock()
	for _, state := range m.tasks {
		// 只有进行中的任务占用目标路径；已完成／失败／取消的历史记录不阻塞重新下载
		//（仓库中已存在同名文件的情况由 buildTarget 单独拦截）。
		if state.task.Path == task.Path && (state.task.Status == "running" || state.task.Status == "paused") {
			m.mu.Unlock()
			return Task{}, &ConflictError{Message: fmt.Sprintf("已有目标为 %s 的任务在进行中", task.Path)}
		}
	}
	m.idSeq++
	task.ID = fmt.Sprintf("dl-%d-%d", task.CreatedAt.Unix(), m.idSeq)
	m.tasks[task.ID] = &taskState{task: task}
	m.persist()
	m.mu.Unlock()

	m.start(task.ID)
	return m.Get(task.ID)
}

// buildTask 校验下载请求并生成任务雏形。
func (m *Manager) buildTask(spec Spec) (Task, error) {
	parsed, err := url.Parse(strings.TrimSpace(spec.URL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return Task{}, &ValidationError{Message: "下载地址必须是合法的 http/https URL"}
	}
	// SSRF 防护（0.0.9.11）：解析目标地址，阻断 loopback／私网／链路本地目标。
	if err := guardSSRF(parsed.Hostname()); err != nil {
		return Task{}, &ValidationError{Message: "下载地址被安全策略拦截：" + err.Error()}
	}
	fileName := strings.TrimSpace(spec.Name)
	if fileName == "" {
		fileName = path.Base(parsed.Path)
	}
	relPath, resolvedName, sum, err := m.buildTarget(spec.Category, fileName, spec.SHA256)
	if err != nil {
		return Task{}, err
	}
	return Task{
		Kind:       "download",
		URL:        parsed.String(),
		Category:   categoryOf(relPath),
		FileName:   resolvedName,
		Path:       relPath,
		Status:     "running",
		TotalBytes: -1,
		SHA256:     sum,
		Message:    "正在建立下载连接",
		CreatedAt:  m.now(),
		UpdatedAt:  m.now(),
	}, nil
}

// buildTarget 校验类别、文件名与校验值，返回仓库相对路径。
func (m *Manager) buildTarget(category, fileName, sum string) (string, string, string, error) {
	category = strings.ToLower(strings.TrimSpace(category))
	if category == "" {
		category = "other"
	}
	if !categoryPattern.MatchString(category) {
		return "", "", "", &ValidationError{Message: "模型类别仅允许小写字母、数字、下划线和连字符"}
	}
	if fileName == "" || fileName == "." || fileName == "/" || strings.ContainsAny(fileName, `/\`) || fileName == ".." || strings.HasPrefix(fileName, ".") {
		return "", "", "", &ValidationError{Message: "模型文件名不合法"}
	}
	sum = strings.ToLower(strings.TrimSpace(sum))
	if sum != "" && !sha256Pattern.MatchString(sum) {
		return "", "", "", &ValidationError{Message: "SHA256 校验值必须是 64 位十六进制字符"}
	}
	relPath := fileName
	if category != "other" {
		relPath = category + "/" + fileName
	}
	if _, err := os.Stat(filepath.Join(m.root, filepath.FromSlash(relPath))); err == nil {
		return "", "", "", &ConflictError{Message: fmt.Sprintf("仓库中已存在文件 %s", relPath)}
	}
	return relPath, fileName, sum, nil
}

// categoryOf 从仓库相对路径推导类别名。
func categoryOf(relPath string) string {
	parts := strings.SplitN(relPath, "/", 2)
	if len(parts) == 2 {
		return parts[0]
	}
	return "other"
}

// start 启动或恢复任务协程。
func (m *Manager) start(id string) {
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	state := m.tasks[id]
	state.cancel = cancel
	state.running = true
	state.stopReason = ""
	kind := state.task.Kind
	m.mu.Unlock()
	if kind == "import" {
		go m.runImport(ctx, id)
		return
	}
	go m.run(ctx, id)
}

// Action 对任务执行 pause／resume／cancel／retry。
func (m *Manager) Action(id, action string) (Task, error) {
	m.mu.Lock()
	state, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return Task{}, &NotFoundError{ID: id}
	}
	status := state.task.Status
	m.mu.Unlock()

	switch action {
	case "pause":
		if status != "running" {
			return Task{}, &ConflictError{Message: "只有下载中的任务可以暂停"}
		}
		m.mu.Lock()
		state.stopReason = "paused"
		state.cancel()
		m.persist()
		m.mu.Unlock()
	case "resume":
		if status != "paused" {
			return Task{}, &ConflictError{Message: "只有已暂停的任务可以继续"}
		}
		m.transition(id, "running", "下载已继续")
		m.start(id)
	case "cancel":
		if status != "running" && status != "paused" {
			return Task{}, &ConflictError{Message: "只有进行中或已暂停的任务可以取消"}
		}
		m.mu.Lock()
		if state.running {
			state.stopReason = "cancelled"
			state.cancel()
		} else {
			state.task.Status = "cancelled"
			state.task.Message = "任务已取消"
			state.task.UpdatedAt = m.now()
			_ = os.Remove(m.partPath(id))
		}
		m.persist()
		m.mu.Unlock()
	case "retry":
		if status != "failed" && status != "cancelled" && status != "paused" {
			return Task{}, &ConflictError{Message: "只有失败、已取消或已暂停的任务可以重试"}
		}
		m.transition(id, "running", "任务已重新进入下载队列")
		m.start(id)
	default:
		return Task{}, &ValidationError{Message: "不支持的操作：" + action}
	}
	return m.Get(id)
}

// Delete 删除终态任务记录。
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.tasks[id]
	if !ok {
		return &NotFoundError{ID: id}
	}
	if state.running || state.task.Status == "running" {
		return &ConflictError{Message: "任务仍在进行，请先取消"}
	}
	delete(m.tasks, id)
	m.persist()
	return nil
}

func (m *Manager) transition(id, status, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state, ok := m.tasks[id]; ok {
		state.task.Status = status
		state.task.Message = message
		state.task.UpdatedAt = m.now()
		m.persist()
	}
}

// partPath 返回任务的临时分片文件路径。
func (m *Manager) partPath(id string) string {
	return filepath.Join(m.root, stagingDirName, id+".part")
}

// run 执行下载；支持断点续传，结束后按 stopReason 落终态。
func (m *Manager) run(ctx context.Context, id string) {
	defer func() {
		m.mu.Lock()
		if state, ok := m.tasks[id]; ok {
			state.running = false
		}
		m.mu.Unlock()
	}()

	partPath := m.partPath(id)
	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		m.fail(id, fmt.Errorf("创建临时目录失败：%w", err))
		return
	}

	var offset int64
	if info, err := os.Stat(partPath); err == nil {
		offset = info.Size()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, m.taskURL(id), nil)
	if err != nil {
		m.fail(id, err)
		return
	}
	if offset > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	response, err := m.client.Do(request)
	if err != nil {
		m.stopOrFail(ctx, id, err)
		return
	}
	defer response.Body.Close()

	switch {
	case response.StatusCode == http.StatusPartialContent:
		// 断点续传成功
	case response.StatusCode == http.StatusOK:
		offset = 0 // 服务端不支持 Range，从头下载
	case response.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		// 本地分片已完整，直接收尾
		m.finish(id, partPath)
		return
	default:
		m.fail(id, fmt.Errorf("下载失败：HTTP %d", response.StatusCode))
		return
	}

	m.mu.Lock()
	if state, ok := m.tasks[id]; ok {
		if response.StatusCode == http.StatusPartialContent {
			state.task.TotalBytes = offset + response.ContentLength
		} else {
			state.task.TotalBytes = response.ContentLength
		}
		state.task.DownloadedBytes = offset
		state.task.Message = "下载中"
		state.task.UpdatedAt = m.now()
	}
	m.mu.Unlock()

	file, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		m.fail(id, err)
		return
	}
	if offset == 0 {
		if err := file.Truncate(0); err != nil {
			file.Close()
			m.fail(id, err)
			return
		}
	} else if _, err := file.Seek(offset, io.SeekStart); err != nil {
		file.Close()
		m.fail(id, err)
		return
	}

	buffer := make([]byte, copyBufferSize)
	downloaded := offset
	for {
		n, readErr := response.Body.Read(buffer)
		if n > 0 {
			if _, err := file.Write(buffer[:n]); err != nil {
				file.Close()
				m.fail(id, err)
				return
			}
			downloaded += int64(n)
			m.mu.Lock()
			if state, ok := m.tasks[id]; ok {
				state.task.DownloadedBytes = downloaded
				state.task.UpdatedAt = m.now()
			}
			m.mu.Unlock()
		}
		if readErr != nil {
			file.Close()
			if errors.Is(readErr, io.EOF) {
				m.finish(id, partPath)
			} else {
				m.stopOrFail(ctx, id, readErr)
			}
			return
		}
	}
}

// runImport 执行本地文件复制导入；恢复时从头复制。
func (m *Manager) runImport(ctx context.Context, id string) {
	defer func() {
		m.mu.Lock()
		if state, ok := m.tasks[id]; ok {
			state.running = false
		}
		m.mu.Unlock()
	}()

	m.mu.Lock()
	state, ok := m.tasks[id]
	var source string
	if ok {
		source = state.task.Source
	}
	m.mu.Unlock()
	if !ok {
		return
	}

	src, err := os.Open(source)
	if err != nil {
		m.fail(id, fmt.Errorf("打开导入文件失败：%w", err))
		return
	}
	defer src.Close()

	partPath := m.partPath(id)
	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		m.fail(id, fmt.Errorf("创建临时目录失败：%w", err))
		return
	}
	file, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		m.fail(id, err)
		return
	}

	buffer := make([]byte, copyBufferSize)
	var copied int64
	for {
		n, readErr := src.Read(buffer)
		if n > 0 {
			if ctx.Err() != nil {
				file.Close()
				m.stopOrFail(ctx, id, ctx.Err())
				return
			}
			if _, err := file.Write(buffer[:n]); err != nil {
				file.Close()
				m.fail(id, err)
				return
			}
			copied += int64(n)
			m.mu.Lock()
			if state, ok := m.tasks[id]; ok {
				state.task.DownloadedBytes = copied
				state.task.UpdatedAt = m.now()
			}
			m.mu.Unlock()
		}
		if readErr != nil {
			file.Close()
			if errors.Is(readErr, io.EOF) {
				m.finish(id, partPath)
			} else {
				m.stopOrFail(ctx, id, readErr)
			}
			return
		}
	}
}

// finish 校验并归档下载完成的文件。
func (m *Manager) finish(id string, partPath string) {
	m.mu.Lock()
	state, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	expected := state.task.SHA256
	relPath := state.task.Path
	kind := state.task.Kind
	downloaded := state.task.DownloadedBytes
	m.mu.Unlock()

	if expected != "" {
		sum, err := fileSHA256(partPath)
		if err != nil {
			m.fail(id, err)
			return
		}
		if sum != expected {
			m.fail(id, fmt.Errorf("SHA256 校验失败：期望 %s，实际 %s", expected, sum))
			return
		}
	}

	target := filepath.Join(m.root, filepath.FromSlash(relPath))
	// 归档前再确认目标不存在：下载期间用户可能手工放入同名文件，
	// os.Rename 在 Linux 上会静默覆盖，必须避免。
	if _, err := os.Stat(target); err == nil {
		m.fail(id, fmt.Errorf("仓库中已存在文件 %s，已保留原文件", relPath))
		return
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		m.fail(id, err)
		return
	}
	if err := os.Rename(partPath, target); err != nil {
		m.fail(id, fmt.Errorf("写入共享仓库失败：%w", err))
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if state, ok := m.tasks[id]; ok {
		state.task.Status = "completed"
		if kind == "import" {
			state.task.Message = "导入完成，已写入共享仓库"
		} else {
			state.task.Message = "下载完成，已写入共享仓库"
		}
		if expected != "" {
			state.task.Message = "SHA256 校验通过，已写入共享仓库"
		}
		if info, err := os.Stat(target); err == nil {
			state.task.DownloadedBytes = info.Size()
			state.task.TotalBytes = info.Size()
		} else {
			state.task.DownloadedBytes = downloaded
		}
		state.task.UpdatedAt = m.now()
		m.persist()
	}
}

// stopOrFail 区分用户主动停止与真实错误。
func (m *Manager) stopOrFail(ctx context.Context, id string, err error) {
	if ctx.Err() != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		if state, ok := m.tasks[id]; ok {
			reason := state.stopReason
			if reason == "" {
				reason = "paused"
			}
			state.task.Status = reason
			if reason == "paused" {
				if state.task.Kind == "import" {
					state.task.Message = "用户暂停了导入，继续时将重新复制"
				} else {
					state.task.Message = "用户暂停了下载，可断点续传"
				}
			} else {
				state.task.Message = "任务已取消"
			}
			state.task.UpdatedAt = m.now()
			if reason == "cancelled" {
				_ = os.Remove(m.partPath(id))
				state.task.DownloadedBytes = 0
			}
			m.persist()
		}
		return
	}
	m.fail(id, err)
}

func (m *Manager) fail(id string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state, ok := m.tasks[id]; ok {
		state.task.Status = "failed"
		state.task.Message = err.Error()
		state.task.UpdatedAt = m.now()
		m.persist()
	}
}

func (m *Manager) taskURL(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state, ok := m.tasks[id]; ok {
		return state.task.URL
	}
	return ""
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// guardSSRF 校验下载目标：解析主机名全部 IP，任一命中内网/保留段即拒绝。
// 白名单主机（FLOWPILOT_DOWNLOAD_ALLOW_HOSTS，逗号分隔）可放行（内网模型源场景）。
func guardSSRF(hostname string) error {
	for _, allowed := range downloadAllowHosts {
		if strings.EqualFold(hostname, allowed) {
			return nil
		}
	}
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return fmt.Errorf("主机名解析失败")
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("目标 %s 位于内网/保留地址段", ip)
		}
	}
	return nil
}

// downloadAllowHosts 下载目标白名单（初始化时设置）。
var downloadAllowHosts = []string{}

// SetDownloadAllowHosts 设置内网模型源白名单。
func SetDownloadAllowHosts(hosts []string) { downloadAllowHosts = hosts }
