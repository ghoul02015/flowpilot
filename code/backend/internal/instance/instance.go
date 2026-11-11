// Package instance 管理 ComfyUI 实例的 Docker 容器生命周期。
//
// 实例即带 flowpilot.instance=true 标签的容器，平台不维护额外数据库，
// 实例配置（镜像、GPU、端口、自启）全部保存在容器标签上，删除容器即删除实例。
package instance

import (
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ghoul02015/flowpilot/internal/docker"
)

const (
	// LabelInstance 标记 ComfyUI 实例容器。
	LabelInstance = "flowpilot.instance"
	// LabelManaged 与平台卸载保护共用的管理标签。
	LabelManaged         = "flowpilot.managed"
	labelName            = "flowpilot.instance.name"
	labelGPU             = "flowpilot.instance.gpu"
	labelPort            = "flowpilot.instance.port"
	labelAutoStart       = "flowpilot.instance.autostart"
	labelModelsTarget    = "flowpilot.instance.models-target"
	labelOutputsTarget   = "flowpilot.instance.outputs-target"
	labelWorkflowsTarget = "flowpilot.instance.workflows-target"
	// labelModelLinks 标记实例具备模型关联挂载（models-linked 目录 + extra_model_paths.yaml）。
	// 0.0.9.4 之前创建的实例无此标签，模型关联功能对它们不可用（挂载创建时固定，重启不生效）。
	labelModelLinks = "flowpilot.instance.model-links"
	// labelUUID 实例稳定对象 ID（0.0.9.7 起）；同时是 workspace 与链接目录名。
	// 旧实例无此标签：仍用共享目录挂载，删除默认保留旧共享数据。
	labelUUID          = "flowpilot.instance.uuid"
	containerPrefix    = "flowpilot-comfyui-"
	stopTimeoutSecond  = 10
	modelsLinkedSubdir = "models-linked"
)

var (
	namePattern   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$`)
	targetPattern = regexp.MustCompile(`^/[a-zA-Z0-9_./-]{1,127}$`)
)

// Spec 是创建实例的请求参数。GPU 为 -1 表示不分配 GPU。
// ModelsTarget 等挂载目标为空时使用管理器默认值。
type Spec struct {
	Name            string `json:"name"`
	Image           string `json:"image"`
	GPU             int    `json:"gpu"`
	Port            int    `json:"port"`
	ContainerPort   int    `json:"containerPort"`
	AutoStart       bool   `json:"autoStart"`
	ModelsTarget    string `json:"modelsTarget"`
	OutputsTarget   string `json:"outputsTarget"`
	WorkflowsTarget string `json:"workflowsTarget"`
}

// Instance 是实例的对外视图。
type Instance struct {
	Name            string `json:"name"`
	ContainerID     string `json:"containerId"`
	ContainerName   string `json:"containerName"`
	Image           string `json:"image"`
	Status          string `json:"status"` // running | stopped | error
	GPU             int    `json:"gpu"`
	Port            int    `json:"port"`
	AutoStart       bool   `json:"autoStart"`
	ModelsTarget    string `json:"modelsTarget"`
	OutputsTarget   string `json:"outputsTarget"`
	WorkflowsTarget string `json:"workflowsTarget"`
	// ModelLinks 表示该实例是否支持「模型关联」（带 models-linked 挂载）。
	ModelLinks bool `json:"modelLinks"`
	// UUID 稳定对象 ID（0.0.9.7 起新实例必有）；空表示旧实例。
	UUID string `json:"uuid,omitempty"`
	// Workspace 实例独立数据目录相对路径（如 instances/<uuid>）；空表示旧式共享挂载。
	Workspace string    `json:"workspace,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Defaults 是创建实例时的推荐默认值，随列表接口返回。
type Defaults struct {
	Image           string `json:"image"`
	ContainerPort   int    `json:"containerPort"`
	DataDir         string `json:"dataDir"`
	ModelsTarget    string `json:"modelsTarget"`
	OutputsTarget   string `json:"outputsTarget"`
	WorkflowsTarget string `json:"workflowsTarget"`
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
	docker          DockerAPI
	gpuCount        GPUCounter
	hostDataDir     string
	defaultImage    string
	containerPort   int
	modelsTarget    string
	outputsTarget   string
	workflowsTarget string

	// 网络存储（NAS 只读仓库）：启用且已挂载时，实例追加网络仓库与
	// extra_model_paths.yaml 两个 bind 挂载，ComfyUI 原生读取，无需拷贝。
	netActive   func() bool
	netHostDir  string
	yamlWriter  func(name, basePath string) (string, error)
	yamlCleaner func(name string)

	// 模型关联快捷层：每个实例独立的符号链接目录。
	// linksHostDir 是挂载源（宿主机视角，bind 用），
	// linksContainerDir 是平台容器内同目录（os 操作用，二者指向同一物理目录）。
	linksHostDir      string
	linksContainerDir string
	// modelRoot 是平台容器内的本地模型仓库根目录，用于关联时的存在性校验。
	modelRoot string
	// workspacesRoot 平台容器内实例独立数据根（如 <dataDir>/instances）；
	// 为空表示未启用独立 workspace（兼容旧装配）。hostWorkspacesRoot 为宿主机侧同目录。
	workspacesRoot     string
	hostWorkspacesRoot string
	// workspaceReuse 返回同名历史实例（默认删除保留数据）的 UUID，
	// 重建时沿用其 workspace；空表示无历史。
	workspaceReuse func(name string) string
	// markOrphan 保留数据删除后立即标记 DB 记录为孤儿（不等待下次同步）。
	markOrphan func(name string)
	linksMu    sync.Mutex
	// busyOps 实例操作互斥：同一实例的创建/启停/重启/删除串行化，
	// 并发请求返回 BusyError（409）。
	busyOps sync.Map

	// 数据底座钩子（0.0.9.5）：创建／删除后尽力即时写库，
	// 遗漏由数据底座的下次同步修复；为空时无副作用（测试与旧装配兼容）。
	// onDelete 携带 purge：彻底删除移除记录；保留数据删除仅标记孤儿（重建可沿用）。
	onCreate func(spec Spec, created Instance)
	onDelete func(name string, purge bool)
}

// NewManager 创建实例管理器；hostDataDir 是宿主机共享数据目录（绑定挂载用宿主机路径），
// modelsTarget 等为共享目录在实例容器内的默认挂载点（按镜像目录约定调整）。
func NewManager(dockerClient DockerAPI, gpuCount GPUCounter, hostDataDir, defaultImage string, defaultContainerPort int, modelsTarget, outputsTarget, workflowsTarget string) *Manager {
	if defaultContainerPort <= 0 {
		defaultContainerPort = 8188
	}
	if modelsTarget == "" {
		modelsTarget = "/data/models"
	}
	if outputsTarget == "" {
		outputsTarget = "/data/outputs"
	}
	if workflowsTarget == "" {
		workflowsTarget = "/data/workflows"
	}
	return &Manager{
		docker:          dockerClient,
		gpuCount:        gpuCount,
		hostDataDir:     hostDataDir,
		defaultImage:    defaultImage,
		containerPort:   defaultContainerPort,
		modelsTarget:    modelsTarget,
		outputsTarget:   outputsTarget,
		workflowsTarget: workflowsTarget,
	}
}

// SetNetworkStorage 注册网络存储挂载钩子；active 为 true 时新实例自动获得
// 网络仓库（只读）与 extra_model_paths.yaml 挂载。
func (m *Manager) SetNetworkStorage(hostDir string, active func() bool, writer func(name, basePath string) (string, error), cleaner func(name string)) {
	m.netHostDir = hostDir
	m.netActive = active
	m.yamlWriter = writer
	m.yamlCleaner = cleaner
}

// SetModelLinks 注册模型关联快捷层的目录：
// hostDir 为宿主机侧链接根目录（bind 挂载源，如 <hostDataDir>/instance-links），
// containerDir 为平台容器内同一目录（如 <dataDir>/instance-links，os 操作使用），
// modelRoot 为平台容器内本地模型仓库根（关联存在性校验）。
func (m *Manager) SetModelLinks(hostDir, containerDir, modelRoot string) {
	m.linksHostDir = hostDir
	m.linksContainerDir = containerDir
	m.modelRoot = modelRoot
}

// BusyError 操作进行中冲突。
type BusyError struct{ Name string }

func (e *BusyError) Error() string { return "实例操作进行中，请稍候再试" }

// tryLockOp 尝试占用实例操作槽；已占用返回 false。
func (m *Manager) tryLockOp(name string) bool {
	_, loaded := m.busyOps.LoadOrStore(name, struct{}{})
	return !loaded
}

// unlockOp 释放实例操作槽。
func (m *Manager) unlockOp(name string) { m.busyOps.Delete(name) }

// SetOrphanMarker 注册保留删除的孤儿标记钩子（数据底座）。
func (m *Manager) SetOrphanMarker(mark func(name string)) {
	m.markOrphan = mark
}

// SetWorkspaceReuse 注册 workspace 复用查询（通常来自数据底座孤儿记录）。
func (m *Manager) SetWorkspaceReuse(reuse func(name string) string) {
	m.workspaceReuse = reuse
}

// SetWorkspaceRoot 注册实例独立数据根目录：
// hostDir 为宿主机侧根（bind 挂载源，如 <hostShared>/instances），
// containerDir 为平台容器内同一路径（os 操作用，如 <dataDir>/instances）。
// 设置后新实例自动使用 instances/<uuid>/{outputs,workflows,user,config} 独立挂载。
func (m *Manager) SetWorkspaceRoot(hostDir, containerDir string) {
	m.hostWorkspacesRoot = hostDir
	m.workspacesRoot = containerDir
}

// SetRecorder 注册数据底座钩子：实例创建成功后回调 onCreate（含完整 Spec），
// 删除成功后回调 onDelete(name, purge)。回调错误由数据层自行记录，不影响实例操作。
func (m *Manager) SetRecorder(onCreate func(spec Spec, created Instance), onDelete func(name string, purge bool)) {
	m.onCreate = onCreate
	m.onDelete = onDelete
}

// Defaults 返回创建表单的推荐默认值。
func (m *Manager) Defaults() Defaults {
	return Defaults{Image: m.defaultImage, ContainerPort: m.containerPort, DataDir: m.hostDataDir, ModelsTarget: m.modelsTarget, OutputsTarget: m.outputsTarget, WorkflowsTarget: m.workflowsTarget}
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
	if !m.tryLockOp(spec.Name) {
		return Instance{}, &BusyError{Name: spec.Name}
	}
	defer m.unlockOp(spec.Name)
	if spec.ContainerPort <= 0 {
		spec.ContainerPort = m.containerPort
	}
	if spec.ModelsTarget == "" {
		spec.ModelsTarget = m.modelsTarget
	}
	if spec.OutputsTarget == "" {
		spec.OutputsTarget = m.outputsTarget
	}
	if spec.WorkflowsTarget == "" {
		spec.WorkflowsTarget = m.workflowsTarget
	}
	if err := m.validate(ctx, spec); err != nil {
		return Instance{}, err
	}

	// 0.0.9.7 起新实例使用独立 workspace：outputs/workflows/user/config 每实例独立，
	// 模型仓库仍全局只读共享。workspace 由后端按 UUID 生成，不接受任意宿主机路径。
	instanceUUID := ""
	binds := []string{
		// 模型仓库只读共享（PRD 10.1）：实例可读取全部模型，
		// 但不能在容器内改动或删除共享文件。
		m.hostDataDir + "/models:" + spec.ModelsTarget + ":ro",
	}
	if m.workspacesRoot != "" && m.hostWorkspacesRoot != "" {
		// 重建沿用：同名历史孤儿记录存在时复用其 UUID 与 workspace。
		if m.workspaceReuse != nil {
			instanceUUID = m.workspaceReuse(spec.Name)
		}
		if instanceUUID == "" {
			instanceUUID = uuid.NewString()
		}
	} else {
		// 未配置 workspace 根（旧装配）：沿用共享目录。
		binds = append(binds,
			m.hostDataDir+"/workflows:"+spec.WorkflowsTarget,
			m.hostDataDir+"/outputs:"+spec.OutputsTarget)
	}
	labels := map[string]string{
		LabelManaged:         "true",
		LabelInstance:        "true",
		labelName:            spec.Name,
		labelGPU:             strconv.Itoa(spec.GPU),
		labelPort:            strconv.Itoa(spec.Port),
		labelAutoStart:       strconv.FormatBool(spec.AutoStart),
		labelModelsTarget:    spec.ModelsTarget,
		labelOutputsTarget:   spec.OutputsTarget,
		labelWorkflowsTarget: spec.WorkflowsTarget,
	}
	if instanceUUID != "" {
		labels[labelUUID] = instanceUUID
	}
	portKey := fmt.Sprintf("%d/tcp", spec.ContainerPort)
	basePath := path.Dir(spec.ModelsTarget)
	if instanceUUID != "" {
		// 独立 workspace：outputs/workflows/user/config 每实例独立目录。
		// hostWorkspacesRoot 已是 <hostShared>/instances，bind 源直接拼 UUID（勿再叠 instances）。
		userTarget := path.Dir(spec.WorkflowsTarget)
		// user 挂载仅在独立子目录时进行（如 /app/ComfyUI/user）；
		// 若 workflows 父目录即 basePath（如 /data/workflows → /data），
		// 挂载 user 会覆盖 basePath 与其他 /data 挂载冲突，跳过。
		mounts := map[string]string{
			"outputs":   spec.OutputsTarget,
			"workflows": spec.WorkflowsTarget,
			"config":    basePath + "/flowpilot-config",
		}
		if userTarget != basePath && userTarget != "/" {
			mounts["user"] = userTarget
		}
		// 四个子目录始终创建（数据完整性）；挂载按布局条件。
		for _, sub := range []string{"outputs", "workflows", "user", "config"} {
			if err := os.MkdirAll(filepath.Join(m.workspacesRoot, instanceUUID, sub), 0o755); err != nil {
				return Instance{}, fmt.Errorf("创建实例数据目录失败：%w", err)
			}
		}
		for sub, target := range mounts {
			binds = append(binds, m.hostWorkspacesRoot+"/"+instanceUUID+"/"+sub+":"+target)
		}
	}
	// extra_model_paths.yaml 恒生成：含 flowpilot_linked 节（模型关联快捷层），
	// 网络存储挂载时另含 flowpilot_network 节，由 writer 内部判定。
	var yamlHostFile string
	if m.yamlWriter != nil {
		hostFile, err := m.yamlWriter(spec.Name, basePath)
		if err != nil {
			return Instance{}, fmt.Errorf("生成模型路径配置失败：%w", err)
		}
		yamlHostFile = hostFile
		binds = append(binds, hostFile+":"+basePath+"/extra_model_paths.yaml:ro")
	}
	// 模型关联快捷层：每实例独立的符号链接目录（只读挂载），
	// 被勾选的共享模型在 ComfyUI 内以 flowpilot_linked 来源出现；增删链接热生效。
	modelLinks := false
	if m.linksHostDir != "" && m.linksContainerDir != "" && yamlHostFile != "" {
		// 链接目录名 0.0.9.7 起用稳定实例 ID；未启用 workspace 的旧装配仍用名称。
		linkDirName := spec.Name
		if instanceUUID != "" {
			linkDirName = instanceUUID
		}
		// 目录在平台容器内创建（与宿主机 bind 源是同一物理目录）。
		if err := os.MkdirAll(filepath.Join(m.linksContainerDir, linkDirName), 0o755); err != nil {
			return Instance{}, fmt.Errorf("创建模型关联目录失败：%w", err)
		}
		binds = append(binds, filepath.Join(m.linksHostDir, linkDirName)+":"+basePath+"/"+modelsLinkedSubdir+":ro")
		modelLinks = true
	}
	// 网络存储已启用并完成挂载时：追加 NAS 只读仓库（yaml 的 network 节由 writer 判定）。
	if m.netActive != nil && m.netActive() && m.netHostDir != "" {
		binds = append(binds, m.netHostDir+":"+basePath+"/models-net:ro")
	}
	if modelLinks {
		labels[labelModelLinks] = "true"
	}
	hostConfig := map[string]any{
		"PortBindings": map[string]any{portKey: []map[string]string{{"HostPort": strconv.Itoa(spec.Port)}}},
		"Binds":        binds,
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
	created, err := m.Get(ctx, spec.Name)
	if err != nil {
		return Instance{}, err
	}
	if m.onCreate != nil {
		m.onCreate(spec, created)
	}
	return created, nil
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
	for _, target := range []string{spec.ModelsTarget, spec.OutputsTarget, spec.WorkflowsTarget} {
		if !targetPattern.MatchString(target) || strings.Contains(target, "..") {
			return &ValidationError{Message: fmt.Sprintf("挂载目标路径不合法：%s", target)}
		}
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
	// 探测宿主机端口真实占用（覆盖管理端口 18080 与其他非实例服务）。
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(spec.Port))
	if err != nil {
		return &ValidationError{Message: fmt.Sprintf("端口 %d 已被宿主机其他服务占用，请更换端口", spec.Port)}
	}
	_ = listener.Close()
	return nil
}

// Action 对实例执行 start／stop／restart。
func (m *Manager) Action(ctx context.Context, name, action string) (Instance, error) {
	if !m.tryLockOp(name) {
		return Instance{}, &BusyError{Name: name}
	}
	defer m.unlockOp(name)
	target, err := m.Get(ctx, name)
	if err != nil {
		return Instance{}, err
	}
	switch action {
	case "start":
		if conflictErr := m.checkGPUConflict(ctx, target); conflictErr != nil {
			return Instance{}, conflictErr
		}
		err = m.docker.StartContainer(ctx, target.ContainerID)
	case "stop":
		err = m.docker.StopContainer(ctx, target.ContainerID, stopTimeoutSecond)
	case "restart":
		if stopErr := m.docker.StopContainer(ctx, target.ContainerID, stopTimeoutSecond); stopErr != nil {
			return Instance{}, fmt.Errorf("停止容器失败：%w", stopErr)
		}
		if conflictErr := m.checkGPUConflict(ctx, target); conflictErr != nil {
			return Instance{}, conflictErr
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

// checkGPUConflict 在启动前校验目标实例的 GPU 未被其他运行中实例占用；
// 仅创建时校验不够——停止后的实例可以在 GPU 已被占用时被再次启动。
func (m *Manager) checkGPUConflict(ctx context.Context, target Instance) error {
	if target.GPU < 0 {
		return nil
	}
	instances, err := m.List(ctx)
	if err != nil {
		return err
	}
	for _, other := range instances {
		if other.Name != target.Name && other.GPU == target.GPU && other.Status == "running" {
			return &ValidationError{Message: fmt.Sprintf("GPU %d 正被运行中的实例 %s 使用，请先停止该实例", target.GPU, other.Name)}
		}
	}
	return nil
}

// Delete 删除实例。0.0.9.7 起分两档：
//   - purge=false（默认）：删除容器与模型链接目录，保留独立 workspace 数据
//     （数据库记录由下次同步标记为孤儿，可恢复或后续彻底清理）。
//   - purge=true：彻底删除容器、链接目录与 workspace 全部数据。
//
// force 为 true 时允许删除运行中的实例。
func (m *Manager) Delete(ctx context.Context, name string, force, purge bool) error {
	if !m.tryLockOp(name) {
		return &BusyError{Name: name}
	}
	defer m.unlockOp(name)
	target, err := m.Get(ctx, name)
	if err != nil {
		return err
	}
	if target.Status == "running" && !force {
		return &ValidationError{Message: "实例正在运行，请先停止或确认强制删除"}
	}
	if m.yamlCleaner != nil {
		m.yamlCleaner(name)
	}
	if m.linksContainerDir != "" {
		// 关联目录仅含平台创建的符号链接，随实例一并清理（兼容旧实例名称目录）。
		linkDirName := target.UUID
		if linkDirName == "" {
			linkDirName = name
		}
		_ = os.RemoveAll(filepath.Join(m.linksContainerDir, linkDirName))
		_ = os.RemoveAll(filepath.Join(m.linksContainerDir, name))
	}
	if err := m.docker.RemoveContainer(ctx, target.ContainerID, force); err != nil {
		return err
	}
	if purge {
		if m.workspacesRoot != "" && target.UUID != "" {
			// 彻底删除：移除实例全部独立数据。
			if err := os.RemoveAll(filepath.Join(m.workspacesRoot, target.UUID)); err != nil {
				return fmt.Errorf("清理实例数据目录失败：%w", err)
			}
		}
		if m.onDelete != nil {
			m.onDelete(name, true)
		} else if m.markOrphan != nil {
			m.markOrphan(name)
		}
	} else if m.markOrphan != nil {
		// 保留数据档：立即标记孤儿（重建时据此沿用 workspace 与 UUID）。
		m.markOrphan(name)
	}
	return nil
}

// WorkspacePath 返回实例独立数据目录（平台容器内路径）；旧实例返回空。
func (m *Manager) WorkspacePath(target Instance) string {
	if m.workspacesRoot == "" || target.UUID == "" {
		return ""
	}
	return filepath.Join(m.workspacesRoot, target.UUID)
}

func fromContainer(container docker.Container) Instance {
	gpuIndex, _ := strconv.Atoi(container.Labels[labelGPU])
	port, _ := strconv.Atoi(container.Labels[labelPort])
	return Instance{
		Name:            container.Labels[labelName],
		ContainerID:     shortID(container.ID),
		ContainerName:   strings.TrimPrefix(firstName(container.Names), "/"),
		Image:           container.Image,
		Status:          mapStatus(container.State),
		GPU:             gpuIndex,
		Port:            port,
		AutoStart:       container.Labels[labelAutoStart] == "true",
		ModelsTarget:    container.Labels[labelModelsTarget],
		OutputsTarget:   container.Labels[labelOutputsTarget],
		WorkflowsTarget: container.Labels[labelWorkflowsTarget],
		ModelLinks:      container.Labels[labelModelLinks] == "true",
		UUID:            container.Labels[labelUUID],
		Workspace:       workspaceLabelValue(container.Labels[labelUUID]),
		CreatedAt:       time.Unix(container.Created, 0),
	}
}

// workspaceLabelValue 由 UUID 推导 workspace 相对路径。
func workspaceLabelValue(instanceUUID string) string {
	if instanceUUID == "" {
		return ""
	}
	return "instances/" + instanceUUID
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
