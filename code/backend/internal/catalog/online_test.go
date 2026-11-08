package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// stubOnlineBases 将两个源站地址替换为测试服务，返回清理函数。
func stubOnlineBases(t *testing.T, msHandler, hfHandler http.Handler) {
	t.Helper()
	msServer := httptest.NewServer(msHandler)
	hfServer := httptest.NewServer(hfHandler)
	oldMS, oldHF := msAPIBase, hfAPIBase
	msAPIBase, hfAPIBase = msServer.URL, hfServer.URL
	t.Cleanup(func() {
		msAPIBase, hfAPIBase = oldMS, oldHF
		msServer.Close()
		hfServer.Close()
	})
}

func TestSearchOnlineModelScope(t *testing.T) {
	stubOnlineBases(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/dolphin/models" {
			t.Errorf("意外请求：%s %s", r.Method, r.URL)
		}
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["Name"] != "MiniMax-H3" {
			t.Errorf("搜索词未透传：%v", payload)
		}
		fmt.Fprint(w, `{"Code":200,"Data":{"Model":{"Models":[{"Path":"Comfy-Org","Name":"MiniMax-H3","ChineseName":"","Downloads":4829971,"LastUpdatedTime":1793707495}],"TotalCount":1}}}`)
	}), http.NotFoundHandler())

	manager := NewManager("")
	repos, err := manager.SearchOnline(context.Background(), SourceModelScope, "MiniMax-H3")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Repo != "Comfy-Org/MiniMax-H3" || repos[0].Downloads != 4829971 {
		t.Fatalf("搜索结果异常：%+v", repos)
	}
	if repos[0].UpdatedAt == "" {
		t.Fatal("更新时间未转换")
	}
}

func TestSearchOnlineHF(t *testing.T) {
	stubOnlineBases(t, http.NotFoundHandler(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("search") != "flux" {
			t.Errorf("搜索词未透传：%s", r.URL)
		}
		fmt.Fprint(w, `[{"id":"black-forest-labs/FLUX.1-dev","downloads":1000,"lastModified":"2026-01-02T03:04:05.000Z"}]`)
	}))

	manager := NewManager("")
	repos, err := manager.SearchOnline(context.Background(), SourceHF, "flux")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Repo != "black-forest-labs/FLUX.1-dev" || repos[0].UpdatedAt != "2026-01-02" {
		t.Fatalf("HF 搜索结果异常：%+v", repos)
	}
}

func TestSearchOnlineValidation(t *testing.T) {
	manager := NewManager("")
	if _, err := manager.SearchOnline(context.Background(), "gitee", "x"); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("未知源应报 ErrInvalidSource，实际：%v", err)
	}
	if _, err := manager.SearchOnline(context.Background(), SourceModelScope, "  "); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("空关键词应报 ErrInvalidQuery，实际：%v", err)
	}
}

func TestOnlineFilesModelScope(t *testing.T) {
	stubOnlineBases(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("Recursive") != "true" {
			t.Errorf("缺少递归参数：%s", r.URL)
		}
		fmt.Fprint(w, `{"Code":200,"Data":{"Files":[
			{"Path":"diffusion_models","Type":"tree","Size":0},
			{"Path":"README.md","Type":"blob","Size":100},
			{"Path":"diffusion_models/minimax_h3_fl2va_pruned_w6a8.safetensors","Type":"blob","Size":17163000000},
			{"Path":"vae/minimax_h3_video_vae_fp16.safetensors","Type":"blob","Size":5210000000}
		]}}`)
	}), http.NotFoundHandler())

	manager := NewManager("")
	files, err := manager.OnlineFiles(context.Background(), SourceModelScope, "Comfy-Org/MiniMax-H3")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("应只保留权重文件，实际：%+v", files)
	}
	// 按大小降序
	if files[0].Name != "minimax_h3_fl2va_pruned_w6a8.safetensors" || files[0].Category != "diffusion_models" {
		t.Fatalf("排序或类别推断错误：%+v", files[0])
	}
	if files[1].Category != "vae" {
		t.Fatalf("VAE 类别推断错误：%+v", files[1])
	}
	want := "https://modelscope.cn/models/Comfy-Org/MiniMax-H3/resolve/master/" + files[0].Path
	// msURL 使用真实域名（测试只替换 API 根），这里断言路径拼接正确。
	if files[0].URL != want {
		t.Fatalf("下载地址拼接错误：%s", files[0].URL)
	}
}

func TestOnlineFilesHF(t *testing.T) {
	stubOnlineBases(t, http.NotFoundHandler(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
			{"path":"FL2VA/text_encoder/model-00001-of-00014.safetensors","type":"file","size":4930000000},
			{"path":"docs/readme.md","type":"file","size":10},
			{"path":"vae/diffusion_pytorch_model.safetensors","type":"file","size":300000000}
		]`)
	}))

	manager := NewManager("")
	files, err := manager.OnlineFiles(context.Background(), SourceHF, "MiniMaxAI/MiniMax-H3")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("应过滤非权重文件，实际：%+v", files)
	}
	// 中间目录 text_encoder 归 text_encoders；vae 目录归 vae。
	if files[0].Category != "text_encoders" || files[1].Category != "vae" {
		t.Fatalf("类别推断错误：%+v", files)
	}
}

func TestOnlineFilesValidation(t *testing.T) {
	manager := NewManager("")
	if _, err := manager.OnlineFiles(context.Background(), SourceModelScope, "../etc"); !errors.Is(err, ErrInvalidRepo) {
		t.Fatalf("非法仓库应报 ErrInvalidRepo，实际：%v", err)
	}
	if _, err := manager.OnlineFiles(context.Background(), "gitee", "a/b"); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("未知源应报 ErrInvalidSource，实际：%v", err)
	}
}

func TestSourcesStatus(t *testing.T) {
	stubOnlineBases(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway) // 5xx 视为不可达
	}))

	manager := NewManager("")
	statuses := manager.SourcesStatus(context.Background())
	if len(statuses) != 2 {
		t.Fatalf("应返回两个源：%+v", statuses)
	}
	if !statuses[0].Reachable || statuses[0].ID != SourceModelScope {
		t.Fatalf("ModelScope 应可达：%+v", statuses[0])
	}
	if statuses[1].Reachable {
		t.Fatalf("HF 5xx 应不可达：%+v", statuses[1])
	}
	// 60 秒内第二次调用应命中缓存（ CheckedAt 相同）。
	again := manager.SourcesStatus(context.Background())
	if again[0].CheckedAt != statuses[0].CheckedAt {
		t.Fatal("状态缓存未生效")
	}
}

func TestGuessCategory(t *testing.T) {
	cases := map[string]string{
		"diffusion_models/minimax_h3_fl2va_pruned_w6a8.safetensors": "diffusion_models",
		"text_encoders/qwen3vl_32b_bf16.safetensors":                "text_encoders",
		"vae/ae.safetensors":                                  "vae",
		"loras/style.safetensors":                             "loras",
		"transformer/model.safetensors":                       "diffusion_models",
		"FL2VA/text_encoder/model-00001-of-00014.safetensors": "text_encoders",
		"v1-5-pruned-emaonly.safetensors":                     "checkpoints",
		"sd_xl_offset_example-lora_1.0.safetensors":           "loras",
		"4x-UltraSharp.pth":                                   "upscale_models",
	}
	for path, want := range cases {
		if got := GuessCategory(path); got != want {
			t.Errorf("GuessCategory(%q) = %q，期望 %q", path, got, want)
		}
	}
}

func TestSourcesStatusTimeout(t *testing.T) {
	slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(6 * time.Second)
		w.WriteHeader(http.StatusOK)
	})
	stubOnlineBases(t, slow, slow)
	manager := NewManager("")
	start := time.Now()
	statuses := manager.SourcesStatus(context.Background())
	if time.Since(start) > 7*time.Second {
		t.Fatal("探测应在 5 秒超时内收口")
	}
	for _, status := range statuses {
		if status.Reachable {
			t.Fatalf("超时源应不可达：%+v", status)
		}
	}
}
