// Package download 实现模型 URL 下载任务引擎：断点续传、暂停继续、
// 可选 SHA256 校验，完成后写入共享模型仓库。
//
// 任务保存在内存中，平台重启后任务列表清空（仓库文件不受影响）。
package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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
	URL             string    `json:"url"`
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
	root   string
	client *http.Client
	mu     sync.Mutex
	tasks  map[string]*taskState
	now    func() time.Time
	idSeq  int64
}

// NewManager 创建下载任务管理器，root 为模型仓库根目录。
func NewManager(root string) *Manager {
	return &Manager{
		root:   root,
		client: &http.Client{}, // 下载可能持续很久，不设整体超时，由 ctx 控制
		tasks:  make(map[string]*taskState),
		now:    time.Now,
	}
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
	m.mu.Lock()
	for _, state := range m.tasks {
		// 只有进行中的任务占用目标路径；已完成／失败／取消的历史记录不阻塞重新下载
		//（仓库中已存在同名文件的情况由 buildTask 单独拦截）。
		if state.task.Path == task.Path && (state.task.Status == "running" || state.task.Status == "paused") {
			m.mu.Unlock()
			return Task{}, &ConflictError{Message: fmt.Sprintf("已有目标为 %s 的下载任务在进行中", task.Path)}
		}
	}
	m.idSeq++
	task.ID = fmt.Sprintf("dl-%d-%d", task.CreatedAt.Unix(), m.idSeq)
	m.tasks[task.ID] = &taskState{task: task}
	m.mu.Unlock()

	m.start(task.ID)
	return m.Get(task.ID)
}

// buildTask 校验请求并生成任务雏形。
func (m *Manager) buildTask(spec Spec) (Task, error) {
	parsed, err := url.Parse(strings.TrimSpace(spec.URL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return Task{}, &ValidationError{Message: "下载地址必须是合法的 http/https URL"}
	}
	category := strings.ToLower(strings.TrimSpace(spec.Category))
	if category == "" {
		category = "other"
	}
	if !categoryPattern.MatchString(category) {
		return Task{}, &ValidationError{Message: "模型类别仅允许小写字母、数字、下划线和连字符"}
	}
	fileName := strings.TrimSpace(spec.Name)
	if fileName == "" {
		fileName = path.Base(parsed.Path)
	}
	if fileName == "" || fileName == "." || fileName == "/" || strings.ContainsAny(fileName, `/\`) || fileName == ".." || strings.HasPrefix(fileName, ".") {
		return Task{}, &ValidationError{Message: "模型文件名不合法"}
	}
	sum := strings.ToLower(strings.TrimSpace(spec.SHA256))
	if sum != "" && !sha256Pattern.MatchString(sum) {
		return Task{}, &ValidationError{Message: "SHA256 校验值必须是 64 位十六进制字符"}
	}
	relPath := fileName
	if category != "other" {
		relPath = category + "/" + fileName
	}
	if _, err := os.Stat(filepath.Join(m.root, filepath.FromSlash(relPath))); err == nil {
		return Task{}, &ConflictError{Message: fmt.Sprintf("仓库中已存在文件 %s", relPath)}
	}
	return Task{
		URL:        parsed.String(),
		Category:   category,
		FileName:   fileName,
		Path:       relPath,
		Status:     "running",
		TotalBytes: -1,
		SHA256:     sum,
		Message:    "正在建立下载连接",
		CreatedAt:  m.now(),
		UpdatedAt:  m.now(),
	}, nil
}

// start 启动或恢复下载协程。
func (m *Manager) start(id string) {
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	state := m.tasks[id]
	state.cancel = cancel
	state.running = true
	state.stopReason = ""
	m.mu.Unlock()
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
	return nil
}

func (m *Manager) transition(id, status, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state, ok := m.tasks[id]; ok {
		state.task.Status = status
		state.task.Message = message
		state.task.UpdatedAt = m.now()
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
		state.task.Message = "下载完成，已写入共享仓库"
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
			state.task.Message = map[string]string{"paused": "用户暂停了下载，可断点续传", "cancelled": "任务已取消"}[reason]
			state.task.UpdatedAt = m.now()
			if reason == "cancelled" {
				_ = os.Remove(m.partPath(id))
				state.task.DownloadedBytes = 0
			}
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
