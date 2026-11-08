// Package catalog 提供主流 ComfyUI 模型的内置目录与 ModelScope 在线刷新。
// 目录条目优先 ModelScope 直链（国内可达），Hugging Face 镜像（hf-mirror）作为备选。
package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry 是一个可一键下载的模型条目。
type Entry struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Category       string `json:"category"` // 共享仓库子目录（checkpoints/loras/vae/...）
	TypeLabel      string `json:"typeLabel"`
	FileName       string `json:"fileName"`
	Description    string `json:"description"`
	SizeBytes      int64  `json:"sizeBytes"`
	Repo           string `json:"repo"`           // ModelScope 仓库（org/name），用于在线刷新
	FilePath       string `json:"filePath"`       // 仓库内文件路径
	URL            string `json:"url"`            // ModelScope 直链（主）
	FallbackURL    string `json:"fallbackUrl"`    // HF 镜像直链（备），可为空
	FallbackGated  bool   `json:"fallbackGated"`  // 备选源是否需要授权
	Reachable      bool   `json:"reachable"`      // 上次刷新的可达性（未刷新时为内置标注）
	RefreshMessage string `json:"refreshMessage"` // 上次刷新结果说明
}

// Catalog 是目录接口的响应结构。
type Catalog struct {
	Entries     []Entry `json:"entries"`
	RefreshedAt string  `json:"refreshedAt"` // 上次在线刷新时间，空表示从未刷新
	Source      string  `json:"source"`      // builtin 或 cache
}

type persistedCatalog struct {
	Entries     []Entry `json:"entries"`
	RefreshedAt string  `json:"refreshedAt"`
}

// Manager 管理目录的内置数据、缓存与在线刷新。
type Manager struct {
	cachePath  string // 空表示不持久化（仅内存）
	client     *http.Client
	mu         sync.RWMutex
	entries    []Entry
	refreshed  time.Time
	loaded     bool
	refreshing bool
}

func NewManager(cachePath string) *Manager {
	return &Manager{
		cachePath: cachePath,
		client:    &http.Client{Timeout: 12 * time.Second},
	}
}

func msURL(repo, path string) string {
	return fmt.Sprintf("https://modelscope.cn/models/%s/resolve/master/%s", repo, path)
}

// msRequestBase 是 ModelScope 文件列表 API 的地址模板（测试中可替换为本地服务）。
var msRequestBase = "https://modelscope.cn/api/v1/models/%s/repo/files?Revision=master"

func hfURL(repo, path string) string {
	return fmt.Sprintf("https://hf-mirror.com/%s/resolve/main/%s", repo, path)
}

// builtin 内置目录：全部条目经 2026-11-08 实机验证仓库存在与文件路径正确，
// 大小取自 ModelScope 文件列表 API 的真实返回值。
func builtin() []Entry {
	type spec struct {
		id, name, category, typeLabel, fileName, description, repo, filePath, hfRepo, hfPath string
		sizeGB                                                                               float64
		gated                                                                                bool
	}
	specs := []spec{
		{"sd15", "Stable Diffusion 1.5", "checkpoints", "Checkpoints", "v1-5-pruned-emaonly.safetensors", "最通用的 SD1.5 基础模型，生态兼容性最好", "AI-ModelScope/stable-diffusion-v1-5", "v1-5-pruned-emaonly.safetensors", "Comfy-Org/stable-diffusion-v1-5-archive", "v1-5-pruned-emaonly-fp16.safetensors", 3.97, false},
		{"sdxl-base", "Stable Diffusion XL Base 1.0", "checkpoints", "Checkpoints", "sd_xl_base_1.0.safetensors", "SDXL 基础模型，1024×1024 原生分辨率", "AI-ModelScope/stable-diffusion-xl-base-1.0", "sd_xl_base_1.0.safetensors", "stabilityai/stable-diffusion-xl-base-1.0", "sd_xl_base_1.0.safetensors", 6.46, false},
		{"sdxl-offset-lora", "SDXL Offset LoRA", "loras", "LoRA", "sd_xl_offset_example-lora_1.0.safetensors", "SDXL 官方示例 LoRA，增强画面风格化", "AI-ModelScope/stable-diffusion-xl-base-1.0", "sd_xl_offset_example-lora_1.0.safetensors", "stabilityai/stable-diffusion-xl-base-1.0", "sd_xl_offset_example-lora_1.0.safetensors", 0.05, false},
		{"flux-dev", "FLUX.1 Dev", "checkpoints", "Checkpoints", "flux1-dev.safetensors", "Black Forest Labs 旗舰文生图模型（需配合 T5/CLIP 编码器）", "AI-ModelScope/FLUX.1-dev", "flux1-dev.safetensors", "black-forest-labs/FLUX.1-dev", "flux1-dev.safetensors", 22.17, true},
		{"flux-schnell", "FLUX.1 Schnell", "checkpoints", "Checkpoints", "flux1-schnell.safetensors", "FLUX 快速版，4 步出图，适合实时预览", "AI-ModelScope/FLUX.1-schnell", "flux1-schnell.safetensors", "black-forest-labs/FLUX.1-schnell", "flux1-schnell.safetensors", 22.15, false},
		{"flux-vae", "FLUX VAE（ae）", "vae", "VAE", "ae.safetensors", "FLUX 系列必备 VAE 解码器", "AI-ModelScope/FLUX.1-schnell", "ae.safetensors", "black-forest-labs/FLUX.1-schnell", "ae.safetensors", 0.31, false},
		{"flux-clip-l", "CLIP-L 文本编码器", "text_encoders", "Text Encoders", "clip_l.safetensors", "FLUX／SD3.5 必需的 CLIP-L 编码器", "AI-ModelScope/flux_text_encoders", "clip_l.safetensors", "comfyanonymous/flux_text_encoders", "clip_l.safetensors", 0.23, false},
		{"flux-t5xxl-fp8", "T5-XXL FP8 文本编码器", "text_encoders", "Text Encoders", "t5xxl_fp8_e4m3fn.safetensors", "FLUX 必需的 T5 编码器，FP8 量化省一半显存", "AI-ModelScope/flux_text_encoders", "t5xxl_fp8_e4m3fn.safetensors", "comfyanonymous/flux_text_encoders", "t5xxl_fp8_e4m3fn.safetensors", 4.56, false},
		{"sd35-large", "Stable Diffusion 3.5 Large", "checkpoints", "Checkpoints", "sd3.5_large.safetensors", "Stability AI 最新一代，文字渲染与构图显著提升", "AI-ModelScope/stable-diffusion-3.5-large", "sd3.5_large.safetensors", "stabilityai/stable-diffusion-3.5-large", "sd3.5_large.safetensors", 15.33, true},
		{"sd-vae-ft-mse", "SD VAE（ft-mse）", "vae", "VAE", "vae-ft-mse-840000-ema-pruned.safetensors", "SD1.5 官方改进版 VAE，色彩细节更好", "AI-ModelScope/sd-vae-ft-mse-original", "vae-ft-mse-840000-ema-pruned.safetensors", "stabilityai/sd-vae-ft-mse-original", "vae-ft-mse-840000-ema-pruned.safetensors", 0.31, false},
		{"controlnet-canny-sd15", "ControlNet Canny（SD1.5）", "controlnet", "ControlNet", "control_v11p_sd15_canny.pth", "边缘线稿控制构图，SD1.5 最常用 ControlNet", "AI-ModelScope/ControlNet-v1-1", "control_v11p_sd15_canny.pth", "lllyasviel/ControlNet-v1-1", "control_v11p_sd15_canny.pth", 1.35, false},
		{"wan21-t2v-1.3b", "Wan2.1 文生视频 1.3B", "diffusion_models", "Diffusion Models", "diffusion_pytorch_model.safetensors", "阿里万相视频生成，1.3B 版本 8GB 显存可跑", "Wan-AI/Wan2.1-T2V-1.3B", "diffusion_pytorch_model.safetensors", "Wan-AI/Wan2.1-T2V-1.3B", "diffusion_pytorch_model.safetensors", 5.29, false},
		{"wan21-vae", "Wan2.1 视频 VAE", "vae", "VAE", "Wan2.1_VAE.pth", "Wan2.1 视频模型配套 VAE", "Wan-AI/Wan2.1-T2V-1.3B", "Wan2.1_VAE.pth", "Wan-AI/Wan2.1-T2V-1.3B", "Wan2.1_VAE.pth", 0.47, false},
		{"realesrgan-x4", "Real-ESRGAN x4 放大", "upscale_models", "Upscale Models", "RealESRGAN_x4.pth", "通用 4 倍高清放大，出图后处理必备", "AI-ModelScope/Real-ESRGAN", "RealESRGAN_x4.pth", "", "", 0.06, false},
	}
	entries := make([]Entry, 0, len(specs))
	for _, s := range specs {
		entry := Entry{
			ID:          s.id,
			Name:        s.name,
			Category:    s.category,
			TypeLabel:   s.typeLabel,
			FileName:    s.fileName,
			Description: s.description,
			SizeBytes:   int64(s.sizeGB * 1024 * 1024 * 1024),
			Repo:        s.repo,
			FilePath:    s.filePath,
			URL:         msURL(s.repo, s.filePath),
			Reachable:   true,
		}
		if s.hfRepo != "" {
			entry.FallbackURL = hfURL(s.hfRepo, s.hfPath)
			entry.FallbackGated = s.gated
		}
		entries = append(entries, entry)
	}
	return entries
}

// List 返回当前目录：优先缓存（含在线刷新结果），否则内置目录。
func (m *Manager) List() Catalog {
	m.mu.Lock()
	if !m.loaded {
		m.loadLocked()
	}
	entries := append([]Entry(nil), m.entries...)
	refreshed := m.refreshed
	m.mu.Unlock()
	source := "builtin"
	if !refreshed.IsZero() {
		source = "cache"
	}
	return Catalog{Entries: entries, RefreshedAt: timeOrEmpty(refreshed), Source: source}
}

func timeOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// loadLocked 读取缓存文件；缓存不存在或损坏时回落内置目录。调用方须持有锁。
func (m *Manager) loadLocked() {
	m.loaded = true
	m.entries = builtin()
	if m.cachePath == "" {
		return
	}
	raw, err := os.ReadFile(m.cachePath)
	if err != nil {
		return
	}
	var saved persistedCatalog
	if err := json.Unmarshal(raw, &saved); err != nil || len(saved.Entries) == 0 {
		return
	}
	// 以缓存为准，但保留内置新增条目（升级后目录扩充仍可见）。
	byID := make(map[string]Entry, len(saved.Entries))
	for _, entry := range saved.Entries {
		byID[entry.ID] = entry
	}
	merged := make([]Entry, 0, len(m.entries)+4)
	seen := make(map[string]bool, len(m.entries))
	for _, entry := range m.entries {
		if cached, ok := byID[entry.ID]; ok {
			merged = append(merged, cached)
		} else {
			merged = append(merged, entry)
		}
		seen[entry.ID] = true
	}
	for _, entry := range saved.Entries {
		if !seen[entry.ID] {
			merged = append(merged, entry)
		}
	}
	m.entries = merged
	if t, err := time.Parse(time.RFC3339, saved.RefreshedAt); err == nil {
		m.refreshed = t
	}
}

// Refresh 逐个条目查询 ModelScope 文件列表 API，更新大小与可达性并写缓存。
// 同一时刻仅允许一个刷新任务；并发调用返回刷新中的当前状态。
func (m *Manager) Refresh(ctx context.Context) (Catalog, error) {
	m.mu.Lock()
	if m.refreshing {
		m.mu.Unlock()
		return Catalog{}, fmt.Errorf("目录正在刷新中，请稍后查询结果")
	}
	m.refreshing = true
	if !m.loaded {
		m.loadLocked()
	}
	base := append([]Entry(nil), m.entries...)
	m.mu.Unlock()

	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range base {
		wg.Add(1)
		sem <- struct{}{}
		go func(entry *Entry) {
			defer wg.Done()
			defer func() { <-sem }()
			m.refreshEntry(ctx, entry)
		}(&base[i])
	}
	wg.Wait()

	m.mu.Lock()
	m.entries = base
	m.refreshed = time.Now()
	m.refreshing = false
	m.persistLocked()
	entries := append([]Entry(nil), m.entries...)
	refreshed := m.refreshed
	m.mu.Unlock()
	return Catalog{Entries: entries, RefreshedAt: timeOrEmpty(refreshed), Source: "cache"}, nil
}

// modelScopeFiles 是 ModelScope 文件列表 API 的响应子集。
type modelScopeFiles struct {
	Code int `json:"Code"`
	Data struct {
		Files []struct {
			Path string `json:"Path"`
			Size int64  `json:"Size"`
			Type string `json:"Type"`
		} `json:"Files"`
	} `json:"Data"`
}

func (m *Manager) refreshEntry(ctx context.Context, entry *Entry) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf(msRequestBase, entry.Repo), nil)
	if err != nil {
		entry.RefreshMessage = "请求构造失败"
		entry.Reachable = false
		return
	}
	response, err := m.client.Do(request)
	if err != nil {
		entry.RefreshMessage = "ModelScope 连接失败"
		entry.Reachable = false
		return
	}
	defer response.Body.Close()
	var body modelScopeFiles
	if err := json.NewDecoder(http.MaxBytesReader(nil, response.Body, 4<<20)).Decode(&body); err != nil || body.Code != 200 {
		entry.RefreshMessage = "ModelScope 仓库查询失败"
		entry.Reachable = false
		return
	}
	for _, file := range body.Data.Files {
		if file.Type == "blob" && file.Path == entry.FilePath {
			if file.Size > 0 {
				entry.SizeBytes = file.Size
			}
			entry.Reachable = true
			entry.RefreshMessage = "已同步 ModelScope 最新信息"
			return
		}
	}
	// 一级目录没找到时尝试全仓库常见子路径前缀匹配（防御仓库结构调整）。
	for _, file := range body.Data.Files {
		if file.Type == "blob" && strings.HasSuffix(file.Path, "/"+entry.FilePath) {
			if file.Size > 0 {
				entry.SizeBytes = file.Size
			}
			entry.Reachable = true
			entry.RefreshMessage = "已同步 ModelScope 最新信息"
			return
		}
	}
	entry.Reachable = false
	entry.RefreshMessage = "ModelScope 仓库中未找到该文件"
}

// persistLocked 原子写缓存。调用方须持有锁。
func (m *Manager) persistLocked() {
	if m.cachePath == "" {
		return
	}
	raw, err := json.MarshalIndent(persistedCatalog{Entries: m.entries, RefreshedAt: timeOrEmpty(m.refreshed)}, "", "  ")
	if err != nil {
		slogError("catalog marshal failed", err)
		return
	}
	tmp := m.cachePath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		slogError("catalog cache write failed", err)
		return
	}
	if err := os.Rename(tmp, m.cachePath); err != nil {
		slogError("catalog cache rename failed", err)
	}
}

func slogError(message string, err error) {
	fmt.Fprintf(os.Stderr, "catalog: %s: %v\n", message, err)
}

// SortByCategory 供测试与调用方整理展示顺序。
func SortByCategory(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Category != entries[j].Category {
			return entries[i].Category < entries[j].Category
		}
		return entries[i].Name < entries[j].Name
	})
}

// EnsureDir 供安装路径初始化缓存目录。
func EnsureDir(cachePath string) error {
	if cachePath == "" {
		return nil
	}
	return os.MkdirAll(filepath.Dir(cachePath), 0o755)
}
