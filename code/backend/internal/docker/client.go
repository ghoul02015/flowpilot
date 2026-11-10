package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	maxResponseBody = 16 << 20
	maxErrorBody    = 4 << 10
)

// Client provides read-only access to a Docker Engine through its Unix socket.
type Client struct {
	httpClient *http.Client
	// streamClient 不设置超时，用于镜像拉取等长时间流式响应。
	streamClient *http.Client
	baseURL      string
}

func NewClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		httpClient:   &http.Client{Transport: transport, Timeout: 10 * time.Second},
		streamClient: &http.Client{Transport: transport},
		baseURL:      "http://docker",
	}
}

type Info struct {
	Name              string `json:"Name"`
	ServerVersion     string `json:"ServerVersion"`
	OperatingSystem   string `json:"OperatingSystem"`
	OSType            string `json:"OSType"`
	Architecture      string `json:"Architecture"`
	NCPU              int    `json:"NCPU"`
	MemTotal          int64  `json:"MemTotal"`
	Containers        int    `json:"Containers"`
	ContainersRunning int    `json:"ContainersRunning"`
	ContainersPaused  int    `json:"ContainersPaused"`
	ContainersStopped int    `json:"ContainersStopped"`
	DockerRootDir     string `json:"DockerRootDir"`
}

type Version struct {
	Version    string `json:"Version"`
	APIVersion string `json:"ApiVersion"`
	OS         string `json:"Os"`
	Arch       string `json:"Arch"`
}

type Container struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Created int64             `json:"Created"`
	Labels  map[string]string `json:"Labels"`
}

type Engine struct {
	Info    Info    `json:"info"`
	Version Version `json:"version"`
}

func (c *Client) Engine(ctx context.Context) (Engine, error) {
	var engine Engine
	if err := c.getJSON(ctx, "/info", &engine.Info); err != nil {
		return Engine{}, fmt.Errorf("read Docker info: %w", err)
	}
	if err := c.getJSON(ctx, "/version", &engine.Version); err != nil {
		return Engine{}, fmt.Errorf("read Docker version: %w", err)
	}
	return engine, nil
}

func (c *Client) Containers(ctx context.Context) ([]Container, error) {
	containers := make([]Container, 0)
	if err := c.getJSON(ctx, "/containers/json?all=1", &containers); err != nil {
		return nil, fmt.Errorf("list Docker containers: %w", err)
	}
	return containers, nil
}

// ContainerInspect 返回容器完整状态。
func (c *Client) ContainerInspect(ctx context.Context, id string) (ContainerInspectResult, error) {
	var result ContainerInspectResult
	body, err := c.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/json", nil)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return result, fmt.Errorf("decode container inspect: %w", err)
	}
	return result, nil
}

type ContainerInspectResult struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	State struct {
		Status    string `json:"Status"`
		Running   bool   `json:"Running"`
		Paused    bool   `json:"Paused"`
		StartedAt string `json:"StartedAt"`
	} `json:"State"`
	Image  string `json:"Image"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
		Tty    bool              `json:"Tty"`
	} `json:"Config"`
	HostConfig struct {
		Binds []string `json:"Binds"`
	} `json:"HostConfig"`
}

// Image 是本机一个镜像的摘要视图（对应 /images/json 条目）。
type Image struct {
	ID       string            `json:"Id"`
	RepoTags []string          `json:"RepoTags"`
	Size     int64             `json:"Size"`
	Created  int64             `json:"Created"`
	Labels   map[string]string `json:"Labels"`
}

// Images 返回本机全部镜像摘要。
func (c *Client) Images(ctx context.Context) ([]Image, error) {
	images := make([]Image, 0)
	if err := c.getJSON(ctx, "/images/json", &images); err != nil {
		return nil, fmt.Errorf("list Docker images: %w", err)
	}
	return images, nil
}

// ImageExists 判断镜像是否在本地存在。
func (c *Client) ImageExists(ctx context.Context, image string) (bool, error) {
	_, err := c.do(ctx, http.MethodGet, "/images/"+image+"/json", nil)
	if err != nil {
		if IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// PullEvent 是镜像拉取过程中的一条进度事件。
type PullEvent struct {
	Status   string `json:"status"`
	Progress string `json:"progress"`
}

// PullImage 拉取镜像并逐条回调进度事件；镜像名可带 tag，缺省为 latest。
// 响应为逐行 JSON 流，任何一行包含 error 即视为失败。
func (c *Client) PullImage(ctx context.Context, image string, onEvent func(PullEvent)) error {
	fromImage, tag := splitImageTag(image)
	path := "/images/create?fromImage=" + url.QueryEscape(fromImage) + "&tag=" + url.QueryEscape(tag)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")

	response, err := c.streamClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
		return &StatusError{StatusCode: response.StatusCode, Message: strings.TrimSpace(string(body))}
	}

	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		var event struct {
			Status   string `json:"status"`
			Progress string `json:"progress"`
			Error    string `json:"error"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if event.Error != "" {
			return errors.New(event.Error)
		}
		if onEvent != nil {
			onEvent(PullEvent{Status: event.Status, Progress: event.Progress})
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取拉取进度失败：%w", err)
	}
	return nil
}

// splitImageTag 拆分镜像名与 tag；未显式指定时返回 latest。
func splitImageTag(image string) (string, string) {
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon > lastSlash {
		return image[:lastColon], image[lastColon+1:]
	}
	return image, "latest"
}

// RemoveImage 删除本机镜像或标签；force 为 true 时允许强制删除。
func (c *Client) RemoveImage(ctx context.Context, image string, force bool) error {
	path := "/images/" + image
	if force {
		path += "?force=1"
	}
	_, err := c.do(ctx, http.MethodDelete, path, nil)
	return err
}

// ImageID 返回镜像 ID；镜像不存在时返回空字符串。
func (c *Client) ImageID(ctx context.Context, name string) (string, error) {
	body, err := c.do(ctx, http.MethodGet, "/images/"+url.PathEscape(name)+"/json", nil)
	if err != nil {
		if IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	var result struct {
		ID string `json:"Id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode image inspect: %w", err)
	}
	return result.ID, nil
}

// DiskUsage 是 Docker 磁盘占用的摘要视图。
type DiskUsage struct {
	ImagesBytes    int64 `json:"imagesBytes"`
	ImagesCount    int   `json:"imagesCount"`
	BuildCacheSize int64 `json:"buildCacheBytes"`
}

// DiskUsage 读取 /system/df，汇总镜像与构建缓存占用。
func (c *Client) DiskUsage(ctx context.Context) (DiskUsage, error) {
	var raw struct {
		LayersSize int64 `json:"LayersSize"`
		Images     []struct {
			Size int64 `json:"Size"`
		} `json:"Images"`
		BuildCache []struct {
			Size int64 `json:"Size"`
		} `json:"BuildCache"`
	}
	if err := c.getJSON(ctx, "/system/df", &raw); err != nil {
		return DiskUsage{}, fmt.Errorf("read Docker disk usage: %w", err)
	}
	usage := DiskUsage{ImagesBytes: raw.LayersSize, ImagesCount: len(raw.Images)}
	for _, cache := range raw.BuildCache {
		usage.BuildCacheSize += cache.Size
	}
	return usage, nil
}

// PruneBuildCache 清理 BuildKit 构建缓存，返回回收的字节数。
func (c *Client) PruneBuildCache(ctx context.Context) (int64, error) {
	body, err := c.do(ctx, http.MethodPost, "/builds/prune", nil)
	if err != nil {
		return 0, fmt.Errorf("清理构建缓存失败：%w", err)
	}
	var result struct {
		SpaceReclaimed int64 `json:"SpaceReclaimed"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("decode build prune response: %w", err)
	}
	return result.SpaceReclaimed, nil
}

// TagImage 为已有镜像增加一个新标签。
func (c *Client) TagImage(ctx context.Context, source, target string) error {
	repo, tag := splitImageTag(target)
	path := "/images/" + source + "/tag?repo=" + url.QueryEscape(repo) + "&tag=" + url.QueryEscape(tag)
	_, err := c.do(ctx, http.MethodPost, path, nil)
	return err
}

// SaveImage 将镜像导出为 tar 流。
func (c *Client) SaveImage(ctx context.Context, image string, writer io.Writer) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/images/"+image+"/get", nil)
	if err != nil {
		return err
	}
	response, err := c.streamClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
		return &StatusError{StatusCode: response.StatusCode, Message: strings.TrimSpace(string(body))}
	}
	_, err = io.Copy(writer, response.Body)
	return err
}

// LoadImage 从 tar 流导入镜像。
func (c *Client) LoadImage(ctx context.Context, reader io.Reader) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/images/load?quiet=0", reader)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-tar")
	response, err := c.streamClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
		return &StatusError{StatusCode: response.StatusCode, Message: strings.TrimSpace(string(body))}
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

// BuildOptions 是 Docker 镜像构建参数。
type BuildOptions struct {
	Tag         string
	Dockerfile  string
	NetworkMode string
	Platform    string
	BuildArgs   map[string]string
}

// BuildEvent 是 Docker 构建流中的一条日志事件。
type BuildEvent struct {
	Stream   string `json:"stream"`
	Status   string `json:"status"`
	Progress string `json:"progress"`
	Error    string `json:"error"`
}

// Message 返回适合界面展示的构建日志文本。
func (e BuildEvent) Message() string {
	if e.Stream != "" {
		return e.Stream
	}
	if e.Status != "" && e.Progress != "" {
		return e.Status + " " + e.Progress
	}
	if e.Status != "" {
		return e.Status
	}
	return e.Error
}

// BuildImage 使用 Docker API 构建镜像并逐条回调日志事件。
func (c *Client) BuildImage(ctx context.Context, tarContext io.Reader, options BuildOptions, onEvent func(BuildEvent)) error {
	query := url.Values{}
	if options.Tag != "" {
		query.Set("t", options.Tag)
	}
	if options.Dockerfile != "" {
		query.Set("dockerfile", options.Dockerfile)
	}
	if options.NetworkMode != "" {
		query.Set("networkmode", options.NetworkMode)
	}
	if options.Platform != "" {
		query.Set("platform", options.Platform)
	}
	if len(options.BuildArgs) > 0 {
		encoded, err := json.Marshal(options.BuildArgs)
		if err != nil {
			return err
		}
		query.Set("buildargs", string(encoded))
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/build?"+query.Encode(), tarContext)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-tar")
	response, err := c.streamClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
		return &StatusError{StatusCode: response.StatusCode, Message: strings.TrimSpace(string(body))}
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var event BuildEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		if event.Error != "" {
			return errors.New(event.Error)
		}
		if onEvent != nil {
			onEvent(event)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取构建日志失败：%w", err)
	}
	return nil
}

// CreateContainer 创建容器并返回容器 ID。
func (c *Client) CreateContainer(ctx context.Context, name string, config any) (string, error) {
	body, err := c.do(ctx, http.MethodPost, "/containers/create?name="+url.QueryEscape(name), config)
	if err != nil {
		return "", err
	}
	var result struct {
		ID string `json:"Id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode container create response: %w", err)
	}
	return result.ID, nil
}

// StartContainer 启动容器；已在运行时 Docker 返回 304，视为成功。
func (c *Client) StartContainer(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/start", nil)
	var statusError *StatusError
	if errors.As(err, &statusError) && statusError.StatusCode == http.StatusNotModified {
		return nil
	}
	return err
}

// StopContainer 停止容器；已停止时 Docker 返回 304，视为成功。
// 优雅停止最长等待 timeoutSeconds，请求本身不套用默认客户端超时；
// 若请求超时但 Docker 实际已完成停止（容器已退出），同样视为成功，
// 保证停止操作幂等，避免大容器触发 502。
func (c *Client) StopContainer(ctx context.Context, id string, timeoutSeconds int) error {
	stopCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds+15)*time.Second)
	defer cancel()
	_, err := c.doWith(c.streamClient, stopCtx, http.MethodPost, fmt.Sprintf("/containers/%s/stop?t=%d", url.PathEscape(id), timeoutSeconds), nil)
	var statusError *StatusError
	if errors.As(err, &statusError) && statusError.StatusCode == http.StatusNotModified {
		return nil
	}
	if err != nil {
		if inspected, inspectErr := c.ContainerInspect(ctx, id); inspectErr == nil && !inspected.State.Running {
			return nil
		}
	}
	return err
}

// RemoveContainer 删除容器，force 为 true 时先强制停止。
func (c *Client) RemoveContainer(ctx context.Context, id string, force bool) error {
	path := "/containers/" + url.PathEscape(id)
	if force {
		path += "?force=1"
	}
	_, err := c.do(ctx, http.MethodDelete, path, nil)
	return err
}

// ContainerTTY 返回容器是否分配了 TTY（决定 logs 流是否需要剥离 multiplex header）。
func (c *Client) ContainerTTY(ctx context.Context, id string) (bool, error) {
	inspected, err := c.ContainerInspect(ctx, id)
	if err != nil {
		return false, err
	}
	return inspected.Config.Tty, nil
}

// ContainerLogs 打开容器日志流：stdout+stderr、带时间戳、先回放 tail 行再 follow。
// 使用无超时的流式客户端；返回的 body 由调用方关闭（关闭即中止流）。
// 非 TTY 容器的流带 8 字节 multiplex header，由 DemuxReader 剥离。
func (c *Client) ContainerLogs(ctx context.Context, id string, tail int) (io.ReadCloser, error) {
	if tail < 0 {
		tail = 0
	}
	path := fmt.Sprintf("/containers/%s/logs?stdout=1&stderr=1&follow=1&timestamps=1&tail=%d", url.PathEscape(id), tail)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	response, err := c.streamClient.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		defer response.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
		message := strings.TrimSpace(string(body))
		var dockerError struct {
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(message), &dockerError) == nil && dockerError.Message != "" {
			message = dockerError.Message
		}
		if message == "" {
			message = http.StatusText(response.StatusCode)
		}
		return nil, &StatusError{StatusCode: response.StatusCode, Message: message}
	}
	return response.Body, nil
}

func (c *Client) getJSON(ctx context.Context, path string, target any) error {
	body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode Docker API response: %w", err)
	}
	return nil
}

// do 执行一次 Docker API 请求并返回响应体；204 空响应返回 nil。
func (c *Client) do(ctx context.Context, method, path string, payload any) ([]byte, error) {
	return c.doWith(c.httpClient, ctx, method, path, payload)
}

// doWith 使用指定 HTTP 客户端执行请求，停止等长耗时操作可绕过默认超时。
func (c *Client) doWith(client *http.Client, ctx context.Context, method, path string, payload any) ([]byte, error) {
	var reader io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		reader = strings.NewReader(string(encoded))
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
		message := strings.TrimSpace(string(body))
		// Docker 错误体为 {"message":"..."}，提取可读信息。
		var dockerError struct {
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(message), &dockerError) == nil && dockerError.Message != "" {
			message = dockerError.Message
		}
		if message == "" {
			message = http.StatusText(response.StatusCode)
		}
		return nil, &StatusError{StatusCode: response.StatusCode, Message: message}
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBody {
		return nil, fmt.Errorf("Docker API response exceeds %d bytes", maxResponseBody)
	}
	return body, nil
}

// StatusError 保留 Docker API 的状态码，便于映射为 404／409 等语义。
type StatusError struct {
	StatusCode int
	Message    string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%d %s: %s", e.StatusCode, http.StatusText(e.StatusCode), e.Message)
}

// IsNotFound 判断错误是否为 Docker 资源不存在。
func IsNotFound(err error) bool {
	var statusError *StatusError
	return errors.As(err, &statusError) && statusError.StatusCode == http.StatusNotFound
}

// IsConflict 判断错误是否为 Docker 资源冲突（如名称已存在）。
func IsConflict(err error) bool {
	var statusError *StatusError
	return errors.As(err, &statusError) && statusError.StatusCode == http.StatusConflict
}
