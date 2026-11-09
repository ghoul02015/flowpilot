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
	Path       string    `json:"path"`     // 相对仓库根目录的路径
	SizeBytes  int64     `json:"sizeBytes"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// Report 是一次仓库扫描结果。
type Report struct {
	Root       string    `json:"root"`
	Files      []File    `json:"files"`
	TotalBytes int64     `json:"totalBytes"`
	ScannedAt  time.Time `json:"scannedAt"`
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

	mu       sync.Mutex
	cached   *Report
	cachedAt time.Time
}

const scanCacheTTL = 2 * time.Second

// NewManager 创建模型仓库管理器，root 为容器内模型目录（如 /data/flowpilot/models）。
func NewManager(root string) *Manager {
	return &Manager{root: root}
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

// scan 实际遍历仓库目录。
func (m *Manager) scan() (Report, error) {
	report := Report{Root: m.root, Files: make([]File, 0), ScannedAt: time.Now()}
	if _, err := os.Stat(m.root); err != nil {
		if os.IsNotExist(err) {
			return report, nil
		}
		return report, fmt.Errorf("读取模型仓库失败：%w", err)
	}
	err := filepath.WalkDir(m.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			if entry.IsDir() && path != m.root {
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
		relative, err := filepath.Rel(m.root, path)
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
		})
		report.TotalBytes += info.Size()
		return nil
	})
	if err != nil {
		return report, fmt.Errorf("扫描模型仓库失败：%w", err)
	}
	sort.Slice(report.Files, func(i, j int) bool { return report.Files[i].Path < report.Files[j].Path })
	return report, nil
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
	return nil
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
