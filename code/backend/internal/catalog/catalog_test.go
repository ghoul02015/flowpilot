package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinCatalogIntegrity(t *testing.T) {
	entries := builtin()
	if len(entries) < 10 {
		t.Fatalf("内置目录条目过少：%d", len(entries))
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if seen[entry.ID] {
			t.Fatalf("条目 ID 重复：%s", entry.ID)
		}
		seen[entry.ID] = true
		if entry.Name == "" || entry.Category == "" || entry.FileName == "" || entry.Repo == "" || entry.FilePath == "" {
			t.Fatalf("条目字段不完整：%+v", entry)
		}
		if !strings.HasPrefix(entry.URL, "https://modelscope.cn/models/") || !strings.HasSuffix(entry.URL, "/"+entry.FilePath) {
			t.Fatalf("ModelScope 直链格式错误：%s", entry.URL)
		}
		if entry.FallbackURL != "" && !strings.HasPrefix(entry.FallbackURL, "https://hf-mirror.com/") {
			t.Fatalf("备选地址必须走 hf-mirror：%s", entry.FallbackURL)
		}
		if entry.SizeBytes <= 0 {
			t.Fatalf("条目缺少内置大小：%s", entry.ID)
		}
	}
}

func TestListFallsBackToBuiltin(t *testing.T) {
	manager := NewManager(filepath.Join(t.TempDir(), "missing", "catalog.json"))
	result := manager.List()
	if result.Source != "builtin" || len(result.Entries) == 0 || result.RefreshedAt != "" {
		t.Fatalf("空缓存应回落内置目录：%+v", result)
	}
}

func TestLoadMergesCacheWithBuiltin(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "catalog.json")
	cached := persistedCatalog{
		RefreshedAt: "2026-11-08T00:00:00Z",
		Entries: []Entry{
			{ID: "sdxl-base", Name: "缓存条目", Category: "checkpoints", FileName: "sd_xl_base_1.0.safetensors", SizeBytes: 123},
			{ID: "legacy-entry", Name: "历史条目", Category: "loras", FileName: "old.safetensors", SizeBytes: 5},
		},
	}
	raw, _ := json.Marshal(cached)
	if err := os.WriteFile(cachePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(cachePath)
	result := manager.List()
	if result.Source != "cache" || result.RefreshedAt != "2026-11-08T00:00:00Z" {
		t.Fatalf("应识别缓存：%+v", result)
	}
	byID := map[string]Entry{}
	for _, entry := range result.Entries {
		byID[entry.ID] = entry
	}
	if byID["sdxl-base"].SizeBytes != 123 {
		t.Fatal("缓存条目应覆盖内置同 ID 条目")
	}
	if _, ok := byID["legacy-entry"]; !ok {
		t.Fatal("缓存中的历史条目应保留")
	}
	if _, ok := byID["sd15"]; !ok {
		t.Fatal("内置新增条目应合并保留")
	}
}

func TestRefreshUpdatesEntriesAndPersists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 模拟 ModelScope 文件列表 API：仅 sdxl-base 仓库有数据。
		if strings.Contains(r.URL.Path, "stable-diffusion-xl-base-1.0") {
			_, _ = w.Write([]byte(`{"Code":200,"Data":{"Files":[{"Path":"sd_xl_base_1.0.safetensors","Size":777,"Type":"blob"}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"Code":100,"Data":{"Files":[]}}`))
	}))
	defer server.Close()

	cachePath := filepath.Join(t.TempDir(), "catalog.json")
	manager := NewManager(cachePath)
	manager.client = server.Client()
	original := msRequestBase
	msRequestBase = server.URL + "/api/v1/models/%s/repo/files?Revision=master"
	defer func() { msRequestBase = original }()

	result, err := manager.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "cache" || result.RefreshedAt == "" {
		t.Fatalf("刷新后应为缓存目录：%+v", result)
	}
	var sdxl *Entry
	for i := range result.Entries {
		if result.Entries[i].ID == "sdxl-base" {
			sdxl = &result.Entries[i]
		}
	}
	if sdxl == nil || !sdxl.Reachable || sdxl.SizeBytes != 777 {
		t.Fatalf("刷新未更新条目：%+v", sdxl)
	}
	// 缓存应持久化并在重启后恢复。
	restarted := NewManager(cachePath)
	loaded := restarted.List()
	if loaded.Source != "cache" {
		t.Fatal("重启后应恢复刷新结果")
	}
	for _, entry := range loaded.Entries {
		if entry.ID == "sdxl-base" && entry.SizeBytes != 777 {
			t.Fatal("持久化的大小未恢复")
		}
	}
}

func TestRefreshRejectsConcurrentCalls(t *testing.T) {
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-block
	}))
	defer server.Close()

	manager := NewManager("")
	manager.client = server.Client()
	original := msRequestBase
	msRequestBase = server.URL + "/%s"
	defer func() { msRequestBase = original }()

	first := make(chan error, 1)
	go func() { _, err := manager.Refresh(context.Background()); first <- err }()
	// 等第一个刷新进入条目查询后再发起第二个。
	for {
		manager.mu.RLock()
		busy := manager.refreshing
		manager.mu.RUnlock()
		if busy {
			break
		}
	}
	if _, err := manager.Refresh(context.Background()); err == nil {
		t.Fatal("并发刷新应被拒绝")
	}
	close(block)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}
