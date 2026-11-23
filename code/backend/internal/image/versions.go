// versions.go 解析 ComfyUI 官方源码版本。
//
// 0.1.0.2：构建选择不再直接使用会漂移的 master 分支，而是把用户选择的
// Release／Tag／Branch／Commit 解析为不可变 commit SHA，并随镜像标签、
// 构建任务与源码档案一起持久化。
package image

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	ComfyRepository       = "Comfy-Org/ComfyUI"
	ComfyRepositoryURL    = "https://github.com/Comfy-Org/ComfyUI"
	comfyVersionCacheTTL  = 10 * time.Minute
	comfyVersionHTTPLimit = 8 * time.Second
)

var fullCommitPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// ComfyVersion 是一份可构建的 ComfyUI 源码版本。
type ComfyVersion struct {
	Ref          string `json:"ref"`
	Kind         string `json:"kind"` // release | tag | branch | commit
	Version      string `json:"version,omitempty"`
	Commit       string `json:"commit"`
	CommitShort  string `json:"commitShort"`
	CommitAt     string `json:"commitAt,omitempty"`
	PublishedAt  string `json:"publishedAt,omitempty"`
	Prerelease   bool   `json:"prerelease"`
	Repository   string `json:"repository"`
	CommitURL    string `json:"commitUrl"`
	SourceURL    string `json:"sourceUrl,omitempty"`
	ResolvedAt   string `json:"resolvedAt,omitempty"`
	DisplayLabel string `json:"displayLabel"`
}

// ComfyVersionCatalog 是构建弹窗使用的在线版本目录。
type ComfyVersionCatalog struct {
	Repository string         `json:"repository"`
	Default    *ComfyVersion  `json:"default,omitempty"`
	Releases   []ComfyVersion `json:"releases"`
	Tags       []ComfyVersion `json:"tags"`
	Branch     *ComfyVersion  `json:"branch,omitempty"`
	FetchedAt  string         `json:"fetchedAt"`
	Source     string         `json:"source"` // online | cache
	Stale      bool           `json:"stale"`
	Warnings   []string       `json:"warnings,omitempty"`
}

// VersionResolver 负责官方版本目录、缓存与引用解析。
type VersionResolver struct {
	apiBase   string
	cachePath string
	client    *http.Client
	now       func() time.Time
	token     string

	mu          sync.Mutex
	cache       *ComfyVersionCatalog
	cacheAt     time.Time
	cacheLoaded bool
	etags       map[string]string
	rawBodies   map[string][]byte
}

// NewVersionResolver 创建解析器；apiBase 为空时使用官方 GitHub API。
func NewVersionResolver(apiBase, cachePath string) *VersionResolver {
	if strings.TrimSpace(apiBase) == "" {
		apiBase = "https://api.github.com"
	}
	return &VersionResolver{
		apiBase:   strings.TrimRight(apiBase, "/"),
		cachePath: cachePath,
		client:    &http.Client{Timeout: comfyVersionHTTPLimit},
		now:       time.Now,
		token:     strings.TrimSpace(os.Getenv("FLOWPILOT_GITHUB_TOKEN")),
		etags:     map[string]string{},
		rawBodies: map[string][]byte{},
	}
}

var (
	defaultVersionResolverMu sync.RWMutex
	defaultVersionResolver   = NewVersionResolver("", "")
)

// ConfigureComfyVersionResolver 在进程启动时配置全局解析器与缓存目录。
func ConfigureComfyVersionResolver(apiBase, cachePath string) {
	defaultVersionResolverMu.Lock()
	defer defaultVersionResolverMu.Unlock()
	defaultVersionResolver = NewVersionResolver(apiBase, cachePath)
}

// SetComfyVersionResolver 供测试替换全局解析器。
func SetComfyVersionResolver(resolver *VersionResolver) {
	defaultVersionResolverMu.Lock()
	defer defaultVersionResolverMu.Unlock()
	defaultVersionResolver = resolver
}

func currentVersionResolver() *VersionResolver {
	defaultVersionResolverMu.RLock()
	defer defaultVersionResolverMu.RUnlock()
	return defaultVersionResolver
}

// ComfyVersions 返回版本目录；refresh=true 时强制在线刷新。
func ComfyVersions(ctx context.Context, refresh bool) (ComfyVersionCatalog, error) {
	return currentVersionResolver().Catalog(ctx, refresh)
}

// ResolveComfyVersion 将引用解析为不可变版本；pinnedCommit 非空时校验并使用该提交。
func ResolveComfyVersion(ctx context.Context, ref, kind, pinnedCommit string) (ComfyVersion, error) {
	return currentVersionResolver().Resolve(ctx, ref, kind, pinnedCommit)
}

// PrepareBuildSource 确保构建参数带有经服务端核验的源码坐标。
func PrepareBuildSource(ctx context.Context, params *BuildParams) error {
	if params == nil {
		return &ValidationError{Message: "缺少构建参数"}
	}
	if params.Source != nil && fullCommitPattern.MatchString(params.Source.Commit) {
		resolved, err := ResolveComfyVersion(ctx, params.ComfyUIRef, params.Source.Kind, params.Source.Commit)
		if err != nil {
			return &ValidationError{Message: "无法确认 ComfyUI 源码版本：" + err.Error()}
		}
		params.Source = &resolved
		return nil
	}
	resolved, err := ResolveComfyVersion(ctx, params.ComfyUIRef, "", "")
	if err != nil {
		return &ValidationError{Message: "无法解析 ComfyUI 版本：" + err.Error()}
	}
	params.ComfyUIRef = resolved.Ref
	params.Source = &resolved
	return nil
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Prerelease  bool   `json:"prerelease"`
	Draft       bool   `json:"draft"`
}

type githubTag struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

type githubBranch struct {
	Name   string `json:"name"`
	Commit struct {
		SHA    string `json:"sha"`
		Commit struct {
			Committer struct {
				Date string `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	} `json:"commit"`
}

type githubCommit struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Committer struct {
			Date string `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
}

// Catalog 返回在线目录或可用的最近缓存。
func (r *VersionResolver) Catalog(ctx context.Context, refresh bool) (ComfyVersionCatalog, error) {
	r.mu.Lock()
	r.loadCacheLocked()
	if !refresh && r.cache != nil && r.now().Sub(r.cacheAt) < comfyVersionCacheTTL {
		cached := cloneCatalog(*r.cache)
		cached.Source = "cache"
		cached.Stale = false
		r.mu.Unlock()
		return cached, nil
	}
	r.mu.Unlock()

	catalog, err := r.fetchCatalog(ctx)
	if err == nil {
		r.mu.Lock()
		r.cache = &catalog
		r.cacheAt = r.now()
		r.mu.Unlock()
		r.saveCache(catalog)
		return catalog, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cache == nil {
		return ComfyVersionCatalog{Repository: ComfyRepository}, err
	}
	cached := cloneCatalog(*r.cache)
	cached.Source = "cache"
	cached.Stale = true
	cached.Warnings = append(cached.Warnings, "在线刷新失败，当前展示最近一次成功缓存："+err.Error())
	return cached, nil
}

// Resolve 将 Release／Tag／Branch／Commit 解析到不可变提交。
func (r *VersionResolver) Resolve(ctx context.Context, ref, kind, pinnedCommit string) (ComfyVersion, error) {
	ref = strings.TrimSpace(ref)
	catalog, catalogErr := r.Catalog(ctx, false)
	if ref == "" {
		if catalogErr != nil || catalog.Default == nil {
			if catalogErr != nil {
				return ComfyVersion{}, catalogErr
			}
			return ComfyVersion{}, errors.New("官方尚未提供稳定 Release，请明确选择版本")
		}
		entry := *catalog.Default
		entry.ResolvedAt = r.now().UTC().Format(time.RFC3339)
		return entry, nil
	}
	if !comfyRefPattern.MatchString(ref) || strings.Contains(ref, "..") {
		return ComfyVersion{}, errors.New("版本引用格式不合法")
	}
	if kind != "" && kind != "release" && kind != "tag" && kind != "branch" && kind != "commit" {
		return ComfyVersion{}, errors.New("不支持的版本来源")
	}
	if pinnedCommit != "" && !fullCommitPattern.MatchString(pinnedCommit) {
		return ComfyVersion{}, errors.New("固定提交必须是完整的 40 位 commit SHA")
	}

	entry, found := findCatalogEntry(catalog, ref)
	if found {
		if pinnedCommit != "" && entry.Kind != "branch" && !strings.EqualFold(entry.Commit, pinnedCommit) {
			return ComfyVersion{}, fmt.Errorf("版本 %s 已解析为 %s，与确认的 %s 不一致，请重新解析", ref, shortCommit(entry.Commit), shortCommit(pinnedCommit))
		}
		if pinnedCommit != "" {
			if !strings.EqualFold(entry.Commit, pinnedCommit) {
				// 分支确认的可能是解析后产生的历史 commit，需确认其真实存在。
				if err := r.verifyCommit(ctx, pinnedCommit); err != nil {
					return ComfyVersion{}, err
				}
			}
			entry.Commit = strings.ToLower(pinnedCommit)
			entry.CommitShort = shortCommit(pinnedCommit)
			if entry.Kind == "branch" {
				entry.DisplayLabel = branchDisplay(entry.Ref, entry.Commit)
			}
		}
		entry.ResolvedAt = r.now().UTC().Format(time.RFC3339)
		return entry, nil
	}

	if pinnedCommit != "" {
		resolved, err := r.commitVersion(ctx, pinnedCommit, kindOrGuess(ref, kind))
		if err != nil {
			return ComfyVersion{}, err
		}
		resolved.ResolvedAt = r.now().UTC().Format(time.RFC3339)
		return resolved, nil
	}
	resolved, err := r.commitVersion(ctx, ref, kindOrGuess(ref, kind))
	if err != nil {
		return ComfyVersion{}, err
	}
	resolved.ResolvedAt = r.now().UTC().Format(time.RFC3339)
	return resolved, nil
}

func (r *VersionResolver) fetchCatalog(ctx context.Context) (ComfyVersionCatalog, error) {
	var (
		releases                      []githubRelease
		tags                          []githubTag
		branch                        githubBranch
		releaseErr, tagErr, branchErr error
	)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		releaseErr = r.getJSON(ctx, "/repos/"+ComfyRepository+"/releases?per_page=30", &releases)
	}()
	go func() {
		defer wg.Done()
		tagErr = r.getJSON(ctx, "/repos/"+ComfyRepository+"/tags?per_page=100", &tags)
	}()
	go func() {
		defer wg.Done()
		branchErr = r.getJSON(ctx, "/repos/"+ComfyRepository+"/branches/master", &branch)
	}()
	wg.Wait()

	if releaseErr != nil && tagErr != nil && branchErr != nil {
		return ComfyVersionCatalog{}, fmt.Errorf("官方版本接口不可用：%v", releaseErr)
	}
	catalog := ComfyVersionCatalog{
		Repository: ComfyRepository,
		Releases:   []ComfyVersion{},
		Tags:       []ComfyVersion{},
		FetchedAt:  r.now().UTC().Format(time.RFC3339),
		Source:     "online",
	}
	tagSHA := map[string]string{}
	for _, tag := range tags {
		if tag.Commit.SHA != "" {
			tagSHA[tag.Name] = tag.Commit.SHA
		}
	}
	releaseNames := map[string]bool{}
	for _, release := range releases {
		if release.Draft {
			continue
		}
		sha := tagSHA[release.TagName]
		if sha == "" {
			if resolved, err := r.resolveCommitAPI(ctx, release.TagName); err == nil {
				sha = resolved.SHA
			}
		}
		if sha == "" {
			catalog.Warnings = append(catalog.Warnings, "Release "+release.TagName+" 未能解析 commit，已跳过")
			continue
		}
		version := newComfyVersion(release.TagName, "release", release.TagName, sha)
		version.PublishedAt = release.PublishedAt
		version.Prerelease = release.Prerelease
		version.SourceURL = release.HTMLURL
		catalog.Releases = append(catalog.Releases, version)
		releaseNames[release.TagName] = true
		if !release.Prerelease && catalog.Default == nil {
			entry := version
			catalog.Default = &entry
		}
	}
	for _, tag := range tags {
		if tag.Name == "" || releaseNames[tag.Name] || tag.Commit.SHA == "" || !comfyRefPattern.MatchString(tag.Name) || strings.Contains(tag.Name, "..") {
			continue
		}
		catalog.Tags = append(catalog.Tags, newComfyVersion(tag.Name, "tag", tag.Name, tag.Commit.SHA))
	}
	if branchErr == nil && branch.Commit.SHA != "" {
		entry := newComfyVersion("master", "branch", "", branch.Commit.SHA)
		entry.CommitAt = branch.Commit.Commit.Committer.Date
		entry.SourceURL = ComfyRepositoryURL + "/tree/master"
		catalog.Branch = &entry
	}
	if releaseErr != nil {
		catalog.Warnings = append(catalog.Warnings, "Release 列表读取失败："+releaseErr.Error())
	}
	if tagErr != nil {
		catalog.Warnings = append(catalog.Warnings, "Tags 列表读取失败："+tagErr.Error())
	}
	if branchErr != nil {
		catalog.Warnings = append(catalog.Warnings, "master 分支解析失败："+branchErr.Error())
	}
	return catalog, nil
}

func (r *VersionResolver) commitVersion(ctx context.Context, ref, kind string) (ComfyVersion, error) {
	commit, err := r.resolveCommitAPI(ctx, ref)
	if err != nil {
		return ComfyVersion{}, fmt.Errorf("无法解析版本 %s：%w", ref, err)
	}
	version := newComfyVersion(ref, kind, versionFromRef(ref, kind), commit.SHA)
	version.CommitAt = commit.Commit.Committer.Date
	version.SourceURL = commit.HTMLURL
	return version, nil
}

func (r *VersionResolver) verifyCommit(ctx context.Context, commit string) error {
	_, err := r.resolveCommitAPI(ctx, commit)
	if err != nil {
		return fmt.Errorf("确认 commit %s 失败：%w", shortCommit(commit), err)
	}
	return nil
}

func (r *VersionResolver) resolveCommitAPI(ctx context.Context, ref string) (githubCommit, error) {
	var commit githubCommit
	if err := r.getJSON(ctx, "/repos/"+ComfyRepository+"/commits/"+url.PathEscape(ref), &commit); err != nil {
		return githubCommit{}, err
	}
	if commit.SHA == "" {
		return githubCommit{}, errors.New("接口返回缺少 commit SHA")
	}
	return commit, nil
}

func (r *VersionResolver) getJSON(ctx context.Context, path string, target any) error {
	body, err := r.getBody(ctx, path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("解析官方版本响应失败：%w", err)
	}
	return nil
}

func (r *VersionResolver) getBody(ctx context.Context, path string) ([]byte, error) {
	r.mu.Lock()
	etag := r.etags[path]
	cachedBody := append([]byte(nil), r.rawBodies[path]...)
	r.mu.Unlock()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.apiBase+path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "FlowPilot/0.1")
	if r.token != "" {
		request.Header.Set("Authorization", "Bearer "+r.token)
	}
	if etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	response, err := r.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified && len(cachedBody) > 0 {
		return cachedBody, nil
	}
	if response.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		if response.StatusCode == http.StatusForbidden && response.Header.Get("X-RateLimit-Remaining") == "0" {
			return nil, fmt.Errorf("GitHub API 限流（可在服务端配置 FLOWPILOT_GITHUB_TOKEN）")
		}
		return nil, fmt.Errorf("GitHub API HTTP %d：%s", response.StatusCode, strings.TrimSpace(string(detail)))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	if newETag := response.Header.Get("ETag"); newETag != "" {
		r.etags[path] = newETag
	}
	r.rawBodies[path] = append([]byte(nil), body...)
	r.mu.Unlock()
	return body, nil
}

type versionCacheFile struct {
	Catalog ComfyVersionCatalog `json:"catalog"`
	SavedAt string              `json:"savedAt"`
}

func (r *VersionResolver) loadCacheLocked() {
	if r.cacheLoaded {
		return
	}
	r.cacheLoaded = true
	if r.cachePath == "" {
		return
	}
	raw, err := os.ReadFile(r.cachePath)
	if err != nil {
		return
	}
	var cached versionCacheFile
	if json.Unmarshal(raw, &cached) != nil {
		return
	}
	cached.Catalog.Source = "cache"
	cached.Catalog.Stale = true
	r.cache = &cached.Catalog
	if parsed, err := time.Parse(time.RFC3339, cached.SavedAt); err == nil {
		r.cacheAt = parsed
	}
}

func (r *VersionResolver) saveCache(catalog ComfyVersionCatalog) {
	if r.cachePath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(r.cachePath), 0o755); err != nil {
		return
	}
	raw, err := json.MarshalIndent(versionCacheFile{Catalog: catalog, SavedAt: r.now().UTC().Format(time.RFC3339)}, "", "  ")
	if err != nil {
		return
	}
	target := r.cachePath + ".tmp"
	if err := os.WriteFile(target, raw, 0o644); err != nil {
		return
	}
	_ = os.Rename(target, r.cachePath)
}

func newComfyVersion(ref, kind, version, commit string) ComfyVersion {
	commit = strings.ToLower(strings.TrimSpace(commit))
	entry := ComfyVersion{
		Ref:         ref,
		Kind:        kind,
		Version:     version,
		Commit:      commit,
		CommitShort: shortCommit(commit),
		Repository:  ComfyRepository,
		CommitURL:   ComfyRepositoryURL + "/commit/" + commit,
	}
	if kind == "branch" {
		entry.DisplayLabel = branchDisplay(ref, commit)
	} else if version != "" {
		entry.DisplayLabel = version + " · " + entry.CommitShort
	} else {
		entry.DisplayLabel = ref + " · " + entry.CommitShort
	}
	return entry
}

func branchDisplay(ref, commit string) string {
	return ref + "@" + shortCommit(commit)
}

func shortCommit(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

func findCatalogEntry(catalog ComfyVersionCatalog, ref string) (ComfyVersion, bool) {
	for _, entry := range catalog.Releases {
		if entry.Ref == ref {
			return entry, true
		}
	}
	for _, entry := range catalog.Tags {
		if entry.Ref == ref {
			return entry, true
		}
	}
	if catalog.Branch != nil && (catalog.Branch.Ref == ref || (ref == "master" && catalog.Branch.Ref == "master")) {
		return *catalog.Branch, true
	}
	return ComfyVersion{}, false
}

func kindOrGuess(ref, kind string) string {
	if kind != "" {
		return kind
	}
	if fullCommitPattern.MatchString(ref) {
		return "commit"
	}
	if ref == "master" {
		return "branch"
	}
	if strings.HasPrefix(strings.ToLower(ref), "v") {
		return "tag"
	}
	return "branch"
}

func versionFromRef(ref, kind string) string {
	switch kind {
	case "release", "tag":
		return ref
	default:
		return ""
	}
}

func cloneCatalog(catalog ComfyVersionCatalog) ComfyVersionCatalog {
	cloned := catalog
	cloned.Releases = append([]ComfyVersion(nil), catalog.Releases...)
	cloned.Tags = append([]ComfyVersion(nil), catalog.Tags...)
	cloned.Warnings = append([]string(nil), catalog.Warnings...)
	if catalog.Default != nil {
		item := *catalog.Default
		cloned.Default = &item
	}
	if catalog.Branch != nil {
		item := *catalog.Branch
		cloned.Branch = &item
	}
	return cloned
}

// ComfyUIRefs 保留旧接口的兼容视图；仅列出已解析的分支与标签。
func ComfyUIRefs(ctx context.Context) []ComfyRef {
	refs := []ComfyRef{}
	catalog, err := ComfyVersions(ctx, false)
	if err == nil {
		if catalog.Branch != nil {
			refs = append(refs, ComfyRef{Name: "master", Type: "branch"})
		}
		for _, entry := range catalog.Releases {
			refs = append(refs, ComfyRef{Name: entry.Ref, Type: "tag"})
		}
		for _, entry := range catalog.Tags {
			refs = append(refs, ComfyRef{Name: entry.Ref, Type: "tag"})
		}
	}
	if len(refs) == 0 {
		refs = append(refs, ComfyRef{Name: "master", Type: "branch"})
	}
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].Name > refs[j].Name })
	return refs
}
