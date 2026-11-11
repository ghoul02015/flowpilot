package image

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	testCommitA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testCommitB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testCommitC = "cccccccccccccccccccccccccccccccccccccccc"
	testBranch  = "dddddddddddddddddddddddddddddddddddddddd"
)

func newVersionFixture(t *testing.T) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	var failing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			http.Error(w, "offline", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/"+ComfyRepository+"/releases"):
			fmt.Fprint(w, `[
  {"tag_name":"v0.46.0-rc1","html_url":"https://example/rc","published_at":"2026-11-11T00:00:00Z","prerelease":true,"draft":false},
  {"tag_name":"v0.45.0","html_url":"https://example/stable","published_at":"2026-11-10T00:00:00Z","prerelease":false,"draft":false}
]`)
		case strings.HasPrefix(r.URL.Path, "/repos/"+ComfyRepository+"/tags"):
			fmt.Fprintf(w, `[
  {"name":"v0.46.0-rc1","commit":{"sha":"%s"}},
  {"name":"v0.45.1","commit":{"sha":"%s"}},
  {"name":"v0.45.0","commit":{"sha":"%s"}}
]`, testCommitB, testCommitC, testCommitA)
		case strings.HasPrefix(r.URL.Path, "/repos/"+ComfyRepository+"/branches/master"):
			fmt.Fprintf(w, `{"name":"master","commit":{"sha":"%s","commit":{"committer":{"date":"2026-11-11T01:00:00Z"}}}}`, testBranch)
		case strings.HasPrefix(r.URL.Path, "/repos/"+ComfyRepository+"/commits/"):
			ref := strings.TrimPrefix(r.URL.Path, "/repos/"+ComfyRepository+"/commits/")
			commit := ref
			if ref == "v0.45.1" {
				commit = testCommitC
			}
			if len(commit) != 40 {
				commit = testCommitC
			}
			fmt.Fprintf(w, `{"sha":"%s","html_url":"https://example/commit/%s","commit":{"committer":{"date":"2026-11-09T00:00:00Z"}}}`, commit, commit)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &failing
}

func TestVersionCatalogJoinsReleasesAndTags(t *testing.T) {
	server, _ := newVersionFixture(t)
	resolver := NewVersionResolver(server.URL, "")
	catalog, err := resolver.Catalog(context.Background(), false)
	if err != nil {
		t.Fatalf("读取版本目录失败：%v", err)
	}
	if catalog.Default == nil || catalog.Default.Ref != "v0.45.0" || catalog.Default.Commit != testCommitA {
		t.Fatalf("默认版本应是最新稳定 Release：%+v", catalog.Default)
	}
	if len(catalog.Releases) != 2 || catalog.Releases[0].Prerelease != true {
		t.Fatalf("Release 列表异常：%+v", catalog.Releases)
	}
	if len(catalog.Tags) != 1 || catalog.Tags[0].Ref != "v0.45.1" || catalog.Tags[0].Kind != "tag" {
		t.Fatalf("仅标签版本应单独列出：%+v", catalog.Tags)
	}
	if catalog.Branch == nil || catalog.Branch.Commit != testBranch || catalog.Branch.DisplayLabel != "master@ddddddd" {
		t.Fatalf("master 分支解析异常：%+v", catalog.Branch)
	}
}

func TestResolveVersionsAndPinnedCommit(t *testing.T) {
	server, _ := newVersionFixture(t)
	resolver := NewVersionResolver(server.URL, "")
	ctx := context.Background()

	release, err := resolver.Resolve(ctx, "v0.45.0", "release", "")
	if err != nil || release.Commit != testCommitA || release.Version != "v0.45.0" {
		t.Fatalf("Release 解析异常：%+v / %v", release, err)
	}
	tag, err := resolver.Resolve(ctx, "v0.45.1", "tag", "")
	if err != nil || tag.Commit != testCommitC {
		t.Fatalf("Tag 解析异常：%+v / %v", tag, err)
	}
	branch, err := resolver.Resolve(ctx, "master", "branch", "")
	if err != nil || branch.Commit != testBranch || !strings.HasPrefix(branch.DisplayLabel, "master@") {
		t.Fatalf("分支解析异常：%+v / %v", branch, err)
	}
	if _, err := resolver.Resolve(ctx, "v0.45.0", "release", testCommitB); err == nil {
		t.Fatal("Release 与确认 commit 不一致时必须拒绝")
	}
	pinned, err := resolver.Resolve(ctx, "master", "branch", testCommitA)
	if err != nil || pinned.Commit != testCommitA {
		t.Fatalf("分支固定到历史 commit 应通过 API 核验：%+v / %v", pinned, err)
	}
	custom, err := resolver.Resolve(ctx, testCommitB, "commit", "")
	if err != nil || custom.Kind != "commit" || custom.Commit != testCommitB {
		t.Fatalf("自定义 commit 解析异常：%+v / %v", custom, err)
	}
	defaultVersion, err := resolver.Resolve(ctx, "", "", "")
	if err != nil || defaultVersion.Ref != "v0.45.0" {
		t.Fatalf("空引用应使用最新稳定 Release：%+v / %v", defaultVersion, err)
	}
}

func TestVersionCatalogCacheFallback(t *testing.T) {
	server, failing := newVersionFixture(t)
	cachePath := filepath.Join(t.TempDir(), "comfyui-versions.json")
	resolver := NewVersionResolver(server.URL, cachePath)
	if _, err := resolver.Catalog(context.Background(), false); err != nil {
		t.Fatalf("首次读取失败：%v", err)
	}
	failing.Store(true)
	stale, err := resolver.Catalog(context.Background(), true)
	if err != nil {
		t.Fatalf("在线失败时应回退缓存：%v", err)
	}
	if !stale.Stale || stale.Source != "cache" || stale.Default == nil || len(stale.Warnings) == 0 {
		t.Fatalf("缓存降级状态异常：%+v", stale)
	}

	freshResolver := NewVersionResolver(server.URL, cachePath)
	reloaded, err := freshResolver.Catalog(context.Background(), false)
	if err != nil {
		t.Fatalf("重启后应可从磁盘缓存回退：%v", err)
	}
	if reloaded.Default == nil || reloaded.Default.Commit != testCommitA {
		t.Fatalf("磁盘缓存内容异常：%+v", reloaded)
	}
}

func TestBuildPinsCommitAndMetadata(t *testing.T) {
	source := testSource()
	source.Ref = "v0.45.0"
	source.Kind = "release"
	source.Version = "v0.45.0"
	source.DisplayLabel = "v0.45.0 · " + source.CommitShort
	params := BuildParams{
		Variant: "nvidia-cu128", ImageName: "flowpilot/comfyui:test",
		ComfyUIRef: "v0.45.0", Source: source, PipIndexURL: "tsinghua", WithManager: "1", Platform: "linux/amd64",
	}
	recipe, err := params.validate()
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := BuildDockerfile(params, recipe)
	for _, fragment := range []string{
		`ARG COMFYUI_COMMIT=`,
		`git fetch --depth 1 origin "$COMFYUI_COMMIT"`,
		`https://gh-proxy.com/https://github.com/Comfy-Org/ComfyUI.git`,
		`git checkout --detach "$COMFYUI_COMMIT"`,
		`test "$(git rev-parse HEAD)" = "$COMFYUI_COMMIT"`,
		`flowpilot.comfyui.commit="` + testSource().Commit + `"`,
		`flowpilot.comfyui.channel="release"`,
		`flowpilot.comfyui.published_at="`,
		`/opt/flowpilot/comfyui-source.json`,
	} {
		if !strings.Contains(dockerfile, fragment) {
			t.Fatalf("Dockerfile 缺少固定版本片段 %q：\n%s", fragment, dockerfile)
		}
	}
	if strings.Contains(dockerfile, `git clone --depth 1 --branch`) {
		t.Fatalf("Dockerfile 不应再按可变分支克隆：\n%s", dockerfile)
	}
	script, err := BuildScript(params)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, `--build-arg COMFYUI_COMMIT='`+testSource().Commit+`'`) {
		t.Fatalf("构建脚本缺少固定 commit 参数：\n%s", script)
	}
}

func TestInstallScriptPinsCommitAndAddsKindLabel(t *testing.T) {
	source := testSource()
	script, err := InstallScript(InstallParams{Variant: "nvidia-cu126", ImageName: "test/comfy:v1", ComfyUIRef: "master", Source: source})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`COMFYUI_COMMIT="${COMFYUI_COMMIT:-` + source.Commit + `}"`,
		`fetch --depth 1 origin "$commit"`,
		`flowpilot.kind="comfyui"`,
		`flowpilot.comfyui.commit="${COMFYUI_COMMIT}"`,
		`COPY comfyui-source.json /opt/flowpilot/comfyui-source.json`,
	} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("安装脚本缺少片段 %q：\n%s", fragment, script)
		}
	}
}
