package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
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

func (c *Client) getJSON(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = http.StatusText(response.StatusCode)
		}
		return fmt.Errorf("Docker API returned %s: %s", response.Status, message)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBody+1))
	if err != nil {
		return err
	}
	if len(body) > maxResponseBody {
		return fmt.Errorf("Docker API response exceeds %d bytes", maxResponseBody)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode Docker API response: %w", err)
	}
	return nil
}
