package docker

import (
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
	baseURL    string
}

func NewClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		httpClient: &http.Client{Transport: transport, Timeout: 10 * time.Second},
		baseURL:    "http://docker",
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
	} `json:"Config"`
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
func (c *Client) StopContainer(ctx context.Context, id string, timeoutSeconds int) error {
	_, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/containers/%s/stop?t=%d", url.PathEscape(id), timeoutSeconds), nil)
	var statusError *StatusError
	if errors.As(err, &statusError) && statusError.StatusCode == http.StatusNotModified {
		return nil
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

	response, err := c.httpClient.Do(request)
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
