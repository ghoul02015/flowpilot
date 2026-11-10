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

// NetworkStatus 是网络存储仓库的扫描状态。
type NetworkStatus struct {
	Root      string `json:"root"`
	Available bool   `json:"available"`
}

// Report 是一次仓库扫描结果（本地仓库＋可用时的网络仓库合并视图）。
type Report struct {
	Root       string         `json:"root"`
	Files      []File         `json:"files"`
	TotalBytes int64          `json:"totalBytes"`
	ScannedAt  time.Time      `json:"scannedAt"`
	Network    *NetworkStatus `json:"network,omitempty"`
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

// Manager 管理共享模型仓库。扫描结果缓存 scanCacheTTL 秒，
// 避免界面轮询对大仓库造成持续全盘 IO；删除操作会主动失效缓存。
type Manager struct {
	root string
	// networkSource 返回网络仓库（容器内路径）与是否可用；nil 表示未配置。
	networkSource func() (string, bool)
	// linksRoot 是实例「模型关联」符号链接根目录（容器内视角）；
	// 删除模型时级联清理各实例指向该文件的链接。
	linksRoot string

	mu       sync.Mutex
	cached   *Report
	cachedAt time.Time
}

const scanCacheTTL = 2 * time.Second

// NewManager 创建模型仓库管理器，root 为容器内模型目录（如 /data/flowpilot/models）。
func NewManager(root string) *Manager {
	return &Manager{root: root}
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

// Root 返回仓库根目录。
func (m *Manager) Root() string { return m.root }

// Scan 遍历仓库，返回全部模型文件；目录不存在时返回空仓库而非报错。
// TTL 内命中缓存直接返回；返回的是副本，调用方修改不影响缓存。
func (m *Manager) Scan() (Report, error) {
	m.mu.Lock()
	if m.cached != nil && time.Since(m.cachedAt) < scanCacheTTL {
		report := *m.cached
		m.mu.Unlock()
		return report, nil
	}
	m.mu.Unlock()

	report, err := m.scan()
	if err != nil {
		return report, err
	}
	m.mu.Lock()
	m.cached = &report
	m.cachedAt = time.Now()
	m.mu.Unlock()
	return report, nil
}

// scan 实际遍历本地仓库，并在网络仓库可用时合并扫描。
func (m *Manager) scan() (Report, error) {
	report := Report{Root: m.root, Files: make([]File, 0), ScannedAt: time.Now()}
	if _, err := os.Stat(m.root); err != nil {
		if !os.IsNotExist(err) {
			return report, fmt.Errorf("读取模型仓库失败：%w", err)
		}
	} else if err := m.scanDir(m.root, "local", &report); err != nil {
		return report, err
	}
	if m.networkSource != nil {
		if networkRoot, ok := m.networkSource(); ok {
			status := &NetworkStatus{Root: networkRoot, Available: true}
			report.Network = status
			if _, err := os.Stat(networkRoot); err != nil {
				status.Available = false
			} else if err := m.scanDir(networkRoot, "network", &report); err != nil {
				// 网络仓库抖动（NAS 脱机等）不拖垮整个扫描，如实标记不可用。
				status.Available = false
			}
		}
	}
	sort.Slice(report.Files, func(i, j int) bool {
		if report.Files[i].Origin != report.Files[j].Origin {
			return report.Files[i].Origin < report.Files[j].Origin
		}
		return report.Files[i].Path < report.Files[j].Path
	})
	return report, nil
}

// scanDir 遍历一个仓库根目录，把文件追加进报告。
func (m *Manager) scanDir(root, origin string, report *Report) error {
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
		report.Files = append(report.Files, File{
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
			report.TotalBytes += info.Size()
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("扫描模型仓库失败：%w", err)
	}
	return nil
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
	m.mu.Lock()
	m.cached = nil
	m.mu.Unlock()
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
