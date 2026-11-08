// Package instance 管理 ComfyUI 实例的 Docker 容器生命周期。
//
// 实例即带 flowpilot.instance=true 标签的容器，平台不维护额外数据库，
// 实例配置（镜像、GPU、端口、自启）全部保存在容器标签上，删除容器即删除实例。
package instance

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ghoul02015/flowpilot/internal/docker"
)

const (
	// LabelInstance 标记 ComfyUI 实例容器。
	LabelInstance = "flowpilot.instance"
	// LabelManaged 与平台卸载保护共用的管理标签。
	LabelManaged      = "flowpilot.managed"
	labelName         = "flowpilot.instance.name"
	labelGPU          = "flowpilot.instance.gpu"
	labelPort         = "flowpilot.instance.port"
	labelAutoStart    = "flowpilot.instance.autostart"
	containerPrefix   = "flowpilot-comfyui-"
	stopTimeoutSecond = 10
)

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$`)

// Spec 是创建实例的请求参数。GPU 为 -1 表示不分配 GPU。
type Spec struct {
	Name          string `json:"name"`
	Image         string `json:"image"`
	GPU           int    `json:"gpu"`
	Port          int    `json:"port"`
	ContainerPort int    `json:"containerPort"`
	AutoStart     bool   `json:"autoStart"`
}

// Instance 是实例的对外视图。
type Instance struct {
	Name          string    `json:"name"`
	ContainerID   string    `json:"containerId"`
	ContainerName string    `json:"containerName"`
	Image         string    `json:"image"`
	Status        string    `json:"status"` // running | stopped | error
	GPU           int       `json:"gpu"`
	Port          int       `json:"port"`
	AutoStart     bool      `json:"autoStart"`
	CreatedAt     time.Time `json:"createdAt"`
}

// Defaults 是创建实例时的推荐默认值，随列表接口返回。
type Defaults struct {
	Image         string `json:"image"`
	ContainerPort int    `json:"containerPort"`
	DataDir       string `json:"dataDir"`
}

// DockerAPI 是实例管理依赖的 Docker 操作子集，便于测试替换。
type DockerAPI interface {
	Containers(ctx context.Context) ([]docker.Container, error)
	ImageExists(ctx context.Context, image string) (bool, error)
	CreateContainer(ctx context.Context, name string, config any) (string, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string, timeoutSeconds int) error
	RemoveContainer(ctx context.Context, id string, force bool) error
}

// GPUCounter 返回当前可用 GPU 数量，通常来自硬件采集器。
type GPUCounter func() int

// Manager 管理实例生命周期。
type Manager struct {
	docker        DockerAPI
	gpuCount      GPUCounter
	hostDataDir   string
	defaultImage  string
	containerPort int
}

// NewManager 创建实例管理器；hostDataDir 是宿主机共享数据目录（绑定挂载用宿主机路径）。
func NewManager(dockerClient DockerAPI, gpuCount GPUCounter, hostDataDir, defaultImage string, defaultContainerPort int) *Manager {
	if defaultContainerPort <= 0 {
		defaultContainerPort = 8188
	}
	return &Manager{
		docker:        dockerClient,
		gpuCount:      gpuCount,
		hostDataDir:   hostDataDir,
		defaultImage:  defaultImage,
		containerPort: defaultContainerPort,
	}
}

// Defaults 返回创建表单的推荐默认值。
func (m *Manager) Defaults() Defaults {
	return Defaults{Image: m.defaultImage, ContainerPort: m.containerPort, DataDir: m.hostDataDir}
}

// List 返回全部实例，按创建时间倒序。
func (m *Manager) List(ctx context.Context) ([]Instance, error) {
	containers, err := m.docker.Containers(ctx)
	if err != nil {
		return nil, err
	}
	instances := make([]Instance, 0)
	for _, container := range containers {
		if container.Labels[LabelInstance] != "true" {
			continue
		}
		instances = append(instances, fromContainer(container))
	}
	sort.Slice(instances, func(i, j int) bool { return instances[i].CreatedAt.After(instances[j].CreatedAt) })
	return instances, nil
}

// Get 按名称返回单个实例。
func (m *Manager) Get(ctx context.Context, name string) (Instance, error) {
	containers, err := m.docker.Containers(ctx)
	if err != nil {
		return Instance{}, err
	}
	for _, container := range containers {
		if container.Labels[LabelInstance] == "true" && container.Labels[labelName] == name {
			return fromContainer(container), nil
		}
	}
	return Instance{}, &NotFoundError{Name: name}
}

// NotFoundError 表示实例不存在。
type NotFoundError struct{ Name string }

func (e *NotFoundError) Error() string { return fmt.Sprintf("实例不存在：%s", e.Name) }

// ValidationError 表示请求参数不合法。
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

// Create 校验参数、创建并启动实例容器。
func (m *Manager) Create(ctx context.Context, spec Spec) (Instance, error) {
	if spec.ContainerPort <= 0 {
		spec.ContainerPort = m.containerPort
	}
	if err := m.validate(ctx, spec); err != nil {
		return Instance{}, err
	}

	labels := map[string]string{
		LabelManaged:   "true",
		LabelInstance:  "true",
		labelName:      spec.Name,
		labelGPU:       strconv.Itoa(spec.GPU),
		labelPort:      strconv.Itoa(spec.Port),
		labelAutoStart: strconv.FormatBool(spec.AutoStart),
	}
	portKey := fmt.Sprintf("%d/tcp", spec.ContainerPort)
	hostConfig := map[string]any{
		"PortBindings": map[string]any{portKey: []map[string]string{{"HostPort": strconv.Itoa(spec.Port)}}},
		"Binds": []string{
			m.hostDataDir + "/models:/data/models",
			m.hostDataDir + "/workflows:/data/workflows",
			m.hostDataDir + "/outputs:/data/outputs",
		},
	}
	if spec.AutoStart {
		hostConfig["RestartPolicy"] = map[string]string{"Name": "unless-stopped"}
	}
	if spec.GPU >= 0 {
		hostConfig["DeviceRequests"] = []map[string]any{{
			"Driver":       "nvidia",
			"DeviceIDs":    []string{strconv.Itoa(spec.GPU)},
			"Capabilities": [][]string{{"gpu"}},
		}}
	}
	config := map[string]any{
		"Image":        spec.Image,
		"Labels":       labels,
		"ExposedPorts": map[string]any{portKey: map[string]any{}},
		"HostConfig":   hostConfig,
	}

	containerName := containerPrefix + spec.Name
	id, err := m.docker.CreateContainer(ctx, containerName, config)
	if err != nil {
		if docker.IsConflict(err) {
			return Instance{}, &ValidationError{Message: "容器名称已存在，请更换实例名称"}
		}
		return Instance{}, fmt.Errorf("创建容器失败：%w", err)
	}
	if err := m.docker.StartContainer(ctx, id); err != nil {
		_ = m.docker.RemoveContainer(ctx, id, true)
		return Instance{}, fmt.Errorf("启动容器失败：%w", err)
	}
	return m.Get(ctx, spec.Name)
}

// validate 校验创建参数。
func (m *Manager) validate(ctx context.Context, spec Spec) error {
	if !namePattern.MatchString(spec.Name) {
		return &ValidationError{Message: "实例名称需以字母或数字开头，仅含字母、数字、下划线和连字符，最长 32 字符"}
	}
	if strings.TrimSpace(spec.Image) == "" {
		return &ValidationError{Message: "镜像不能为空"}
	}
	if spec.Port < 1024 || spec.Port > 65535 {
		return &ValidationError{Message: "访问端口需在 1024–65535 之间"}
	}
	if spec.ContainerPort < 1 || spec.ContainerPort > 65535 {
		return &ValidationError{Message: "容器端口需在 1–65535 之间"}
	}
	if spec.GPU >= 0 && m.gpuCount != nil && spec.GPU >= m.gpuCount() {
		return &ValidationError{Message: fmt.Sprintf("GPU %d 不存在，当前主机共 %d 块 GPU", spec.GPU, m.gpuCount())}
	}
	exists, err := m.docker.ImageExists(ctx, spec.Image)
	if err != nil {
		return fmt.Errorf("检查镜像失败：%w", err)
	}
	if !exists {
		return &ValidationError{Message: fmt.Sprintf("镜像 %s 不在本地；离线环境请先导入镜像", spec.Image)}
	}
	instances, err := m.List(ctx)
	if err != nil {
		return err
	}
	for _, existing := range instances {
		if existing.Name == spec.Name {
			return &ValidationError{Message: "实例名称已存在，请更换名称"}
		}
		if existing.Port == spec.Port {
			return &ValidationError{Message: fmt.Sprintf("端口 %d 已被实例 %s 占用", spec.Port, existing.Name)}
		}
		if spec.GPU >= 0 && existing.GPU == spec.GPU && existing.Status == "running" {
			return &ValidationError{Message: fmt.Sprintf("GPU %d 正被运行中的实例 %s 使用", spec.GPU, existing.Name)}
		}
	}
	return nil
}

// Action 对实例执行 start／stop／restart。
func (m *Manager) Action(ctx context.Context, name, action string) (Instance, error) {
	target, err := m.Get(ctx, name)
	if err != nil {
		return Instance{}, err
	}
	switch action {
	case "start":
		err = m.docker.StartContainer(ctx, target.ContainerID)
	case "stop":
		err = m.docker.StopContainer(ctx, target.ContainerID, stopTimeoutSecond)
	case "restart":
		if stopErr := m.docker.StopContainer(ctx, target.ContainerID, stopTimeoutSecond); stopErr != nil {
			return Instance{}, fmt.Errorf("停止容器失败：%w", stopErr)
		}
		err = m.docker.StartContainer(ctx, target.ContainerID)
	default:
		return Instance{}, &ValidationError{Message: "不支持的操作：" + action}
	}
	if err != nil {
		return Instance{}, err
	}
	return m.Get(ctx, name)
}

// Delete 删除实例容器；force 为 true 时允许删除运行中的实例。
func (m *Manager) Delete(ctx context.Context, name string, force bool) error {
	target, err := m.Get(ctx, name)
	if err != nil {
		return err
	}
	if target.Status == "running" && !force {
		return &ValidationError{Message: "实例正在运行，请先停止或确认强制删除"}
	}
	return m.docker.RemoveContainer(ctx, target.ContainerID, force)
}

func fromContainer(container docker.Container) Instance {
	gpuIndex, _ := strconv.Atoi(container.Labels[labelGPU])
	port, _ := strconv.Atoi(container.Labels[labelPort])
	return Instance{
		Name:          container.Labels[labelName],
		ContainerID:   shortID(container.ID),
		ContainerName: strings.TrimPrefix(firstName(container.Names), "/"),
		Image:         container.Image,
		Status:        mapStatus(container.State),
		GPU:           gpuIndex,
		Port:          port,
		AutoStart:     container.Labels[labelAutoStart] == "true",
		CreatedAt:     time.Unix(container.Created, 0),
	}
}

func mapStatus(state string) string {
	switch state {
	case "running":
		return "running"
	case "restarting":
		return "running"
	case "dead", "removing":
		return "error"
	default:
		return "stopped"
	}
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func firstName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return names[0]
}
