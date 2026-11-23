// Package model 扫描并维护宿主机共享模型仓库目录。
//
// 仓库根目录下的第一层子目录即模型类别（checkpoints、loras、vae 等），
// 与 ComfyUI 的 models 目录约定一致；实例容器将该目录挂载为 /data/models。
package model

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// File 是仓库中的一个模型文件。
type File struct {
	Name       string    `json:"name"`
	Category   string    `json:"category"` // 原始子目录名，如 checkpoints
	Type       string    `json:"type"`     // 展示类型，如 Checkpoints、LoRA
	Path       string    `json:"path"`     // 相对所属仓库根目录的路径
	SizeBytes  int64     `json:"sizeBytes"`
	ModifiedAt time.Time `json:"modifiedAt"`
	// Origin 标识来源仓库：local（本地仓库，可写）｜ network（网络存储，只读）。
	Origin string `json:"origin"`
	// Node 预留集群字段，当前恒为「本机」。
	Node string `json:"node"`
}

// SourceStatus 描述单个仓库来源的扫描状态。
// 本地与网络仓库分别维护，避免 NAS 故障把本地结果一起标成不可用。
type SourceStatus struct {
	Origin    string    `json:"origin"`
	Root      string    `json:"root"`
	Available bool      `json:"available"`
	ScannedAt time.Time `json:"scannedAt,omitempty"`
	Stale     bool      `json:"stale"`
	Error     string    `json:"error,omitempty"`
}

// NetworkStatus 保留旧字段名，等价于网络来源状态。
type NetworkStatus = SourceStatus

// Report 是一次仓库读取结果（本地仓库＋可用时的网络仓库合并视图）。
type Report struct {
	Root       string        `json:"root"`
	Files      []File        `json:"files"`
	TotalBytes int64         `json:"totalBytes"`
	ScannedAt  time.Time     `json:"scannedAt"`
	Local      SourceStatus  `json:"local"`
	Network    *SourceStatus `json:"network,omitempty"`
}

// categoryTitles 将 ComfyUI 约定子目录映射为界面展示类型。
var categoryTitles = map[string]string{
	"checkpoints":      "Checkpoints",
	"loras":            "LoRA",
	"vae":              "VAE",
	"controlnet":       "ControlNet",
	"embeddings":       "Embeddings",
	"upscale_models":   "Upscale Models",
	"text_encoders":    "Text Encoders",
	"diffusion_models": "Diffusion Models",
	"clip_vision":      "CLIP Vision",
	"model_patches":    "Model Patches",
}

// ValidationError 表示请求参数不合法。
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

// NotFoundError 表示模型文件不存在。
type NotFoundError struct{ Path string }

func (e *NotFoundError) Error() string { return fmt.Sprintf("模型文件不存在：%s", e.Path) }

const (
	defaultLocalTTL   = 60 * time.Second
	defaultNetworkTTL = 120 * time.Second
)

// sourceCache 是单个来源的扫描结果；errText 非空表示本次扫描失败，
// 此时尽量保留上一次成功结果并标记为过期。
type sourceCache struct {
	files     []File
	total     int64
	scannedAt time.Time
	available bool
	errText   string
}

// sourceState 负责单来源的缓存与并发合并。
type sourceState struct {
	mu       sync.Mutex
	cache    *sourceCache
	scanning bool
	wait     chan struct{}
}

// Manager 管理共享模型仓库。本地与网络仓库各自缓存，
// 同一来源并发读取复用同一次扫描；写入后只失效受影响来源。
type Manager struct {
	root string
	// networkSource 返回网络仓库（容器内路径）与是否可用；nil 表示未配置。
	networkSource func() (string, bool)
	// linksRoot 是实例「模型关联」符号链接根目录（容器内视角）；
	// 删除模型时级联清理各实例指向该文件的链接。
	linksRoot string

	local      sourceState
	network    sourceState
	localTTL   time.Duration
	networkTTL time.Duration
}

// NewManager 创建模型仓库管理器，root 为容器内模型目录（如 /data/flowpilot/models）。
func NewManager(root string) *Manager {
	return &Manager{root: root, localTTL: defaultLocalTTL, networkTTL: defaultNetworkTTL}
}

// SetNetworkSource 注册网络仓库来源（NAS 只读），由网络存储管理器提供。
func (m *Manager) SetNetworkSource(provider func() (string, bool)) {
	m.networkSource = provider
}

// SetLinksRoot 注册实例模型关联的符号链接根目录（容器内视角），
// 删除模型文件时级联清理各实例中指向它的链接。
func (m *Manager) SetLinksRoot(root string) {
	m.linksRoot = root
}

// SetCacheTTL 覆盖缓存有效期；测试使用，ttl<=0 表示不缓存。
func (m *Manager) SetCacheTTL(local, network time.Duration) {
	m.localTTL = local
	m.networkTTL = network
}

// Root 返回仓库根目录。
func (m *Manager) Root() string { return m.root }

// InvalidateLocal 让本地缓存立即过期（下载、导入、删除后调用）。
func (m *Manager) InvalidateLocal() {
	m.local.mu.Lock()
	if m.local.cache != nil {
		m.local.cache.scannedAt = time.Time{}
	}
	m.local.mu.Unlock()
}

// InvalidateNetwork 让网络仓库缓存立即过期（NAS 配置变化后调用）。
func (m *Manager) InvalidateNetwork() {
	m.network.mu.Lock()
	if m.network.cache != nil {
		m.network.cache.scannedAt = time.Time{}
	}
	m.network.mu.Unlock()
}

// Scan 返回本地与网络仓库的合并视图；NAS 失败不影响本地结果。
func (m *Manager) Scan() (Report, error) {
	report := Report{Root: m.root, Files: make([]File, 0), ScannedAt: time.Now()}
	report.Local = SourceStatus{Origin: "local", Root: m.root}

	localCache, localErr := m.loadSource(&m.local, "local", m.root, m.localTTL)
	if localCache != nil {
		report.Files = append(report.Files, localCache.files...)
		report.TotalBytes = localCache.total
		report.Local.Available = localCache.available
		report.Local.ScannedAt = localCache.scannedAt
		report.Local.Error = localCache.errText
		report.Local.Stale = localCache.errText != "" && len(localCache.files) > 0
		if localCache.scannedAt.After(report.ScannedAt) || report.ScannedAt.IsZero() {
			report.ScannedAt = localCache.scannedAt
		}
	}
	var scanErr error
	if localErr != nil && (localCache == nil || (!localCache.available && len(localCache.files) == 0)) {
		scanErr = localErr
	}

	if m.networkSource != nil {
		if networkRoot, ok := m.networkSource(); ok {
			status := SourceStatus{Origin: "network", Root: networkRoot}
			cache, _ := m.loadSource(&m.network, "network", networkRoot, m.networkTTL)
			if cache != nil {
				status.Available = cache.available
				status.ScannedAt = cache.scannedAt
				status.Error = cache.errText
				status.Stale = cache.errText != "" && len(cache.files) > 0
				// 仅在可用时合并网络文件，避免 NAS 故障时把旧文件当成可读。
				if cache.available {
					report.Files = append(report.Files, cache.files...)
					if cache.scannedAt.After(report.ScannedAt) {
						report.ScannedAt = cache.scannedAt
					}
				}
			}
			report.Network = &status
		}
	}

	sort.Slice(report.Files, func(i, j int) bool {
		if report.Files[i].Origin != report.Files[j].Origin {
			return report.Files[i].Origin < report.Files[j].Origin
		}
		return report.Files[i].Path < report.Files[j].Path
	})
	return report, scanErr
}

// loadSource 读取单来源缓存；过期时执行一次扫描，并发调用共享同一结果。
func (m *Manager) loadSource(state *sourceState, origin, root string, ttl time.Duration) (*sourceCache, error) {
	state.mu.Lock()
	if ttl > 0 && state.cache != nil && time.Since(state.cache.scannedAt) < ttl {
		copy := *state.cache
		state.mu.Unlock()
		return &copy, nil
	}
	if state.scanning {
		wait := state.wait
		state.mu.Unlock()
		<-wait
		state.mu.Lock()
		var copy *sourceCache
		if state.cache != nil {
			value := *state.cache
			copy = &value
		}
		state.mu.Unlock()
		return copy, nil
	}
	state.scanning = true
	wait := make(chan struct{})
	state.wait = wait
	state.mu.Unlock()

	files, total, err := m.scanDir(root, origin)

	state.mu.Lock()
	if err != nil {
		if state.cache != nil {
			state.cache.errText = err.Error()
			state.cache.available = false
			state.cache.scannedAt = time.Now()
		} else {
			state.cache = &sourceCache{available: false, errText: err.Error(), scannedAt: time.Now()}
		}
	} else {
		state.cache = &sourceCache{files: files, total: total, available: true, scannedAt: time.Now()}
	}
	value := *state.cache
	state.scanning = false
	state.wait = nil
	close(wait)
	state.mu.Unlock()
	return &value, err
}

// scanDir 遍历一个仓库根目录；目录不存在视为空仓库，网络来源则标记不可用。
func (m *Manager) scanDir(root, origin string) ([]File, int64, error) {
	files := make([]File, 0)
	var total int64
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			if origin == "network" {
				return files, 0, fmt.Errorf("网络仓库不可用：%s", root)
			}
			return files, 0, nil
		}
		return files, 0, fmt.Errorf("读取模型仓库失败：%w", err)
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			if entry.IsDir() && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil // 扫描期间文件被移动，跳过
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		category := categoryOf(relative)
		files = append(files, File{
			Name:       name,
			Category:   category,
			Type:       typeOf(category),
			Path:       filepath.ToSlash(relative),
			SizeBytes:  info.Size(),
			ModifiedAt: info.ModTime(),
			Origin:     origin,
			Node:       "本机",
		})
		if origin == "local" {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		return files, total, fmt.Errorf("扫描模型仓库失败：%w", err)
	}
	return files, total, nil
}

// Delete 物理删除仓库中的一个文件；relPath 必须位于仓库内。
func (m *Manager) Delete(relPath string) error {
	target, err := m.resolve(relPath)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return &NotFoundError{Path: relPath}
		}
		return fmt.Errorf("读取模型文件失败：%w", err)
	}
	if !info.Mode().IsRegular() {
		return &ValidationError{Message: "只能删除模型文件，不能删除目录"}
	}
	if err := os.Remove(target); err != nil {
		return fmt.Errorf("删除模型文件失败：%w", err)
	}
	m.InvalidateLocal()
	m.cleanupInstanceLinks(relPath)
	return nil
}

// cleanupInstanceLinks 级联清理各实例模型中指向被删文件的符号链接。
// 链接目录结构为 <linksRoot>/<实例>/<类别>/<相对路径>；
// 清理失败不阻塞删除本身（残留链接在 ComfyUI 中表现为失效项，可手动重设关联清除）。
func (m *Manager) cleanupInstanceLinks(relPath string) {
	if m.linksRoot == "" {
		return
	}
	cleaned := filepath.Clean(filepath.FromSlash(relPath))
	if cleaned == "." || filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return
	}
	instances, err := os.ReadDir(m.linksRoot)
	if err != nil {
		return
	}
	for _, instanceEntry := range instances {
		if !instanceEntry.IsDir() {
			continue
		}
		linkPath := filepath.Join(m.linksRoot, instanceEntry.Name(), cleaned)
		info, statErr := os.Lstat(linkPath)
		if statErr != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		if os.Remove(linkPath) != nil {
			continue
		}
		pruneEmptyParents(filepath.Dir(linkPath), filepath.Join(m.linksRoot, instanceEntry.Name()))
	}
}

// pruneEmptyParents 自底向上删除空目录，直到停在 root（不含）。
func pruneEmptyParents(dir, root string) {
	for {
		cleanedDir := filepath.Clean(dir)
		cleanedRoot := filepath.Clean(root)
		if cleanedDir == cleanedRoot || !strings.HasPrefix(cleanedDir, cleanedRoot+string(filepath.Separator)) {
			return
		}
		entries, err := os.ReadDir(cleanedDir)
		if err != nil || len(entries) > 0 {
			return
		}
		if os.Remove(cleanedDir) != nil {
			return
		}
		dir = filepath.Dir(cleanedDir)
	}
}

// resolve 将相对路径解析为仓库内的绝对路径，拒绝路径穿越。
func (m *Manager) resolve(relPath string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(relPath))
	if cleaned == "." || filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", &ValidationError{Message: "非法的模型路径"}
	}
	return filepath.Join(m.root, cleaned), nil
}

// categoryOf 返回相对路径的第一层子目录作为类别；根目录文件归为 other。
func categoryOf(relative string) string {
	parts := strings.SplitN(filepath.ToSlash(relative), "/", 2)
	if len(parts) == 2 {
		return strings.ToLower(parts[0])
	}
	return "other"
}

// typeOf 返回类别的展示名称。
func typeOf(category string) string {
	if title, ok := categoryTitles[category]; ok {
		return title
	}
	if category == "other" {
		return "其他"
	}
	return category
}
