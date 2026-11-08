// 在线模型源：实时可达性探测、源站搜索与仓库文件列表（0.0.6.1）。
// 目录从「内置维护」升级为「在线拉取」：源站模型更新后平台无需发版即可看到。
// ModelScope 优先（国内可达），HF 镜像（hf-mirror.com）备选。
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// 源站标识。
const (
	SourceModelScope = "modelscope"
	SourceHF         = "hf"
)

// 源站 API 根地址（测试中可替换为 httptest 服务）。
var (
	msAPIBase = "https://modelscope.cn"
	hfAPIBase = "https://hf-mirror.com"
)

// 参数校验错误，server 层映射为 400。
var (
	ErrInvalidSource = errors.New("未知模型源（仅支持 modelscope / hf）")
	ErrInvalidQuery  = errors.New("搜索关键词为空或过长")
	ErrInvalidRepo   = errors.New("仓库标识不合法")
)

var repoPathPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}/[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// SourceStatus 描述一个模型源的实时可达性。
type SourceStatus struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Home      string `json:"home"`
	Reachable bool   `json:"reachable"`
	LatencyMs int64  `json:"latencyMs"`
	CheckedAt string `json:"checkedAt"`
}

// OnlineRepo 是一次在线搜索命中的模型仓库。
type OnlineRepo struct {
	Source      string `json:"source"`
	Repo        string `json:"repo"`
	Name        string `json:"name"`
	ChineseName string `json:"chineseName,omitempty"`
	Downloads   int64  `json:"downloads"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

// OnlineFile 是仓库内一个可下载的权重文件。
type OnlineFile struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
	Category  string `json:"category"`
	URL       string `json:"url"`
}

// 权重文件扩展名：文件列表只保留可下载的模型权重。
var weightExtensions = []string{".safetensors", ".pth", ".pt", ".gguf", ".bin", ".ckpt"}

func isWeightFile(path string) bool {
	lower := strings.ToLower(path)
	for _, ext := range weightExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// directoryCategory 将 ComfyUI 约定目录（Comfy-Org 仓库布局）映射为入库类别。
var directoryCategory = map[string]string{
	"checkpoints":      "checkpoints",
	"loras":            "loras",
	"vae":              "vae",
	"controlnet":       "controlnet",
	"embeddings":       "embeddings",
	"upscale_models":   "upscale_models",
	"text_encoders":    "text_encoders",
	"diffusion_models": "diffusion_models",
	"model_patches":    "model_patches",
	"clip":             "text_encoders",
	"clip_vision":      "clip_vision",
	"unet":             "diffusion_models",
	"transformer":      "diffusion_models",
}

// GuessCategory 依据仓库内路径推断 ComfyUI 入库类别（与官方模板目录约定对齐）。
// 先按路径逐段匹配约定目录（覆盖 HF 常见的 FL2VA/text_encoder 这类中间段），
// 再按文件名关键词兜底。
func GuessCategory(path string) string {
	lower := strings.ToLower(path)
	for _, segment := range strings.Split(lower, "/") {
		if category, ok := directoryCategory[segment]; ok {
			return category
		}
	}
	base := lower
	if idx := strings.LastIndex(lower, "/"); idx >= 0 {
		base = lower[idx+1:]
	}
	switch {
	case strings.Contains(base, "lora"):
		return "loras"
	case strings.Contains(base, "controlnet"):
		return "controlnet"
	case strings.Contains(lower, "text_encoder"), strings.Contains(base, "clip_"), strings.Contains(base, "t5xxl"):
		return "text_encoders"
	case strings.Contains(base, "vae"):
		return "vae"
	case strings.Contains(base, "esrgan"), strings.Contains(base, "swinir"), strings.Contains(base, "upscale"), strings.Contains(base, "ultrasharp"):
		return "upscale_models"
	case strings.Contains(base, "embedding"):
		return "embeddings"
	}
	if strings.Contains(lower, "/") {
		return "diffusion_models"
	}
	return "checkpoints"
}

// SourcesStatus 并发探测两个源站的实时可达性，60 秒内复用结果。
func (m *Manager) SourcesStatus(ctx context.Context) []SourceStatus {
	m.mu.RLock()
	cached := m.status
	fresh := !m.statusAt.IsZero() && time.Since(m.statusAt) < 60*time.Second
	m.mu.RUnlock()
	if fresh && len(cached) > 0 {
		return append([]SourceStatus(nil), cached...)
	}

	probes := []struct {
		id, name, home, target string
	}{
		{SourceModelScope, "ModelScope", "modelscope.cn", msAPIBase + "/api/v1/models/Wan-AI/Wan2.1-T2V-1.3B"},
		{SourceHF, "HF 镜像", "hf-mirror.com", hfAPIBase + "/api/models?search=wan&limit=1"},
	}
	results := make([]SourceStatus, len(probes))
	checkedAt := time.Now().UTC()
	var wg sync.WaitGroup
	for i, probe := range probes {
		wg.Add(1)
		go func(index int, id, name, home, target string) {
			defer wg.Done()
			probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			start := time.Now()
			reachable := false
			request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, target, nil)
			if err == nil {
				if response, err := m.client.Do(request); err == nil {
					response.Body.Close()
					reachable = response.StatusCode < 500
				}
			}
			results[index] = SourceStatus{
				ID:        id,
				Name:      name,
				Home:      home,
				Reachable: reachable,
				LatencyMs: time.Since(start).Milliseconds(),
				CheckedAt: checkedAt.Format(time.RFC3339),
			}
		}(i, probe.id, probe.name, probe.home, probe.target)
	}
	wg.Wait()

	m.mu.Lock()
	m.status = results
	m.statusAt = time.Now()
	m.mu.Unlock()
	return append([]SourceStatus(nil), results...)
}

// msSearchResponse 是 ModelScope 搜索接口（PUT /api/v1/dolphin/models）的响应子集。
type msSearchResponse struct {
	Code int `json:"Code"`
	Data struct {
		Model struct {
			Models []struct {
				Path            string `json:"Path"`
				Name            string `json:"Name"`
				ChineseName     string `json:"ChineseName"`
				Downloads       int64  `json:"Downloads"`
				LastUpdatedTime int64  `json:"LastUpdatedTime"`
			} `json:"Models"`
		} `json:"Model"`
	} `json:"Data"`
}

// SearchOnline 在指定源站搜索模型仓库，返回归一化结果（按下载量排序）。
func (m *Manager) SearchOnline(ctx context.Context, source, query string) ([]OnlineRepo, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 100 {
		return nil, ErrInvalidQuery
	}
	switch source {
	case SourceModelScope:
		return m.searchModelScope(ctx, query)
	case SourceHF:
		return m.searchHF(ctx, query)
	default:
		return nil, ErrInvalidSource
	}
}

func (m *Manager) searchModelScope(ctx context.Context, query string) ([]OnlineRepo, error) {
	payload := fmt.Sprintf(`{"PageSize":20,"PageNumber":1,"SortBy":"Default","Target":"","SingleCriterion":[],"Name":%s,"Criterion":[]}`, mustJSON(query))
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, msAPIBase+"/api/v1/dolphin/models", strings.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := m.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("ModelScope 搜索请求失败：%w", err)
	}
	defer response.Body.Close()
	var body msSearchResponse
	if err := json.NewDecoder(http.MaxBytesReader(nil, response.Body, 4<<20)).Decode(&body); err != nil || body.Code != 200 {
		return nil, fmt.Errorf("ModelScope 搜索响应异常（HTTP %d）", response.StatusCode)
	}
	repos := make([]OnlineRepo, 0, len(body.Data.Model.Models))
	for _, item := range body.Data.Model.Models {
		repo := item.Path + "/" + item.Name
		repos = append(repos, OnlineRepo{
			Source:      SourceModelScope,
			Repo:        repo,
			Name:        repo,
			ChineseName: item.ChineseName,
			Downloads:   item.Downloads,
			UpdatedAt:   unixOrEmpty(item.LastUpdatedTime),
		})
	}
	return repos, nil
}

func (m *Manager) searchHF(ctx context.Context, query string) ([]OnlineRepo, error) {
	target := fmt.Sprintf("%s/api/models?search=%s&limit=20", hfAPIBase, url.QueryEscape(query))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	response, err := m.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("HF 镜像搜索请求失败：%w", err)
	}
	defer response.Body.Close()
	var body []struct {
		ID           string `json:"id"`
		Downloads    int64  `json:"downloads"`
		LastModified string `json:"lastModified"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, response.Body, 4<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("HF 镜像搜索响应异常（HTTP %d）", response.StatusCode)
	}
	repos := make([]OnlineRepo, 0, len(body))
	for _, item := range body {
		updated := item.LastModified
		if len(updated) > 10 {
			updated = updated[:10]
		}
		repos = append(repos, OnlineRepo{
			Source:    SourceHF,
			Repo:      item.ID,
			Name:      item.ID,
			Downloads: item.Downloads,
			UpdatedAt: updated,
		})
	}
	return repos, nil
}

// OnlineFiles 列出仓库内全部权重文件（含真实大小与推断类别，按大小降序）。
func (m *Manager) OnlineFiles(ctx context.Context, source, repo string) ([]OnlineFile, error) {
	if !repoPathPattern.MatchString(repo) {
		return nil, ErrInvalidRepo
	}
	switch source {
	case SourceModelScope:
		return m.filesModelScope(ctx, repo)
	case SourceHF:
		return m.filesHF(ctx, repo)
	default:
		return nil, ErrInvalidSource
	}
}

func (m *Manager) filesModelScope(ctx context.Context, repo string) ([]OnlineFile, error) {
	target := fmt.Sprintf("%s/api/v1/models/%s/repo/files?Revision=master&Recursive=true", msAPIBase, repo)
	var body modelScopeFiles
	if err := m.getJSON(ctx, target, &body); err != nil {
		return nil, fmt.Errorf("ModelScope 文件列表请求失败：%w", err)
	}
	if body.Code != 200 {
		return nil, fmt.Errorf("ModelScope 仓库不存在或不可访问")
	}
	files := make([]OnlineFile, 0, 16)
	for _, file := range body.Data.Files {
		if file.Type != "blob" || !isWeightFile(file.Path) {
			continue
		}
		files = append(files, OnlineFile{
			Path:      file.Path,
			Name:      baseName(file.Path),
			SizeBytes: file.Size,
			Category:  GuessCategory(file.Path),
			URL:       msURL(repo, file.Path),
		})
	}
	return sortFiles(files), nil
}

// hfTreeNode 是 HF 仓库树接口的响应子集。
type hfTreeNode struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

func (m *Manager) filesHF(ctx context.Context, repo string) ([]OnlineFile, error) {
	target := fmt.Sprintf("%s/api/models/%s/tree/main?recursive=true", hfAPIBase, repo)
	var body []hfTreeNode
	if err := m.getJSON(ctx, target, &body); err != nil {
		return nil, fmt.Errorf("HF 镜像文件列表请求失败：%w", err)
	}
	files := make([]OnlineFile, 0, 16)
	for _, node := range body {
		if node.Type != "file" || !isWeightFile(node.Path) {
			continue
		}
		files = append(files, OnlineFile{
			Path:      node.Path,
			Name:      baseName(node.Path),
			SizeBytes: node.Size,
			Category:  GuessCategory(node.Path),
			URL:       hfURL(repo, node.Path),
		})
	}
	return sortFiles(files), nil
}

func (m *Manager) getJSON(ctx context.Context, target string, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	response, err := m.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return json.NewDecoder(http.MaxBytesReader(nil, response.Body, 16<<20)).Decode(out)
}

func sortFiles(files []OnlineFile) []OnlineFile {
	sort.SliceStable(files, func(i, j int) bool { return files[i].SizeBytes > files[j].SizeBytes })
	if len(files) > 100 {
		return files[:100]
	}
	return files
}

func baseName(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

func unixOrEmpty(seconds int64) string {
	if seconds <= 0 {
		return ""
	}
	return time.Unix(seconds, 0).UTC().Format("2006-01-02")
}

func mustJSON(text string) string {
	raw, err := json.Marshal(text)
	if err != nil {
		return `""`
	}
	return string(raw)
}
