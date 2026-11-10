package instance

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ghoul02015/flowpilot/internal/docker"
)

type fakeDocker struct {
	containers    []docker.Container
	images        map[string]bool
	created       []string
	lastConfig    map[string]any
	started       []string
	stopped       []string
	removed       []string
	createCounter int
	failStart     bool
}

func (f *fakeDocker) Containers(context.Context) ([]docker.Container, error) {
	return f.containers, nil
}

func (f *fakeDocker) ImageExists(_ context.Context, image string) (bool, error) {
	return f.images[image], nil
}

func (f *fakeDocker) CreateContainer(_ context.Context, name string, config any) (string, error) {
	f.createCounter++
	f.created = append(f.created, name)
	// 容器标签优先取真实 config 中的 Labels（新代码路径），回退到兼容默认。
	labels := map[string]string{
		LabelInstance:  "true",
		labelName:      strings.TrimPrefix(name, containerPrefix),
		labelGPU:       "0",
		labelPort:      "18188",
		labelAutoStart: "true",
	}
	image := "comfyui:test"
	if decoded, ok := config.(map[string]any); ok {
		f.lastConfig = decoded
		if configLabels, ok := decoded["Labels"].(map[string]string); ok {
			for key, value := range configLabels {
				labels[key] = value
			}
		}
		if configImage, ok := decoded["Image"].(string); ok && configImage != "" {
			image = configImage
		}
	}
	id := strings.Repeat("ab", 6)
	f.containers = append(f.containers, docker.Container{
		ID:      id,
		Names:   []string{"/" + name},
		Image:   image,
		State:   "created",
		Labels:  labels,
		Created: time.Now().Unix(),
	})
	return id, nil
}

func (f *fakeDocker) StartContainer(_ context.Context, id string) error {
	if f.failStart {
		return errors.New("start failed")
	}
	f.started = append(f.started, id)
	for index := range f.containers {
		if f.containers[index].ID == id {
			f.containers[index].State = "running"
		}
	}
	return nil
}

func (f *fakeDocker) StopContainer(_ context.Context, id string, _ int) error {
	f.stopped = append(f.stopped, id)
	for index := range f.containers {
		if f.containers[index].ID == id {
			f.containers[index].State = "exited"
		}
	}
	return nil
}

func (f *fakeDocker) RemoveContainer(_ context.Context, id string, _ bool) error {
	f.removed = append(f.removed, id)
	kept := f.containers[:0]
	for _, container := range f.containers {
		if container.ID != id {
			kept = append(kept, container)
		}
	}
	f.containers = kept
	return nil
}

func newManager(dockerAPI DockerAPI, gpuCount int) *Manager {
	return NewManager(dockerAPI, func() int { return gpuCount }, "/host/data", "comfyui:test", 8188, "", "", "")
}

func validSpec() Spec {
	return Spec{Name: "draw-01", Image: "comfyui:test", GPU: 0, Port: 18188, ContainerPort: 8188, AutoStart: true}
}

// 宿主机端口被非实例服务（如管理端口）占用时，创建应被拒绝。
func TestCreateRejectsOccupiedHostPort(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	occupiedPort := listener.Addr().(*net.TCPAddr).Port

	fake := &fakeDocker{images: map[string]bool{"comfyui:test": true}}
	manager := newManager(fake, 1)
	spec := validSpec()
	spec.Port = occupiedPort
	var validation *ValidationError
	if _, err := manager.Create(context.Background(), spec); !errors.As(err, &validation) {
		t.Fatalf("宿主机端口占用应返回 ValidationError：%v", err)
	}
}

func TestCreateInstanceSuccess(t *testing.T) {
	fake := &fakeDocker{images: map[string]bool{"comfyui:test": true}}
	manager := newManager(fake, 1)
	created, err := manager.Create(context.Background(), validSpec())
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "draw-01" || created.Status != "running" || created.GPU != 0 || created.Port != 18188 {
		t.Fatalf("unexpected instance: %+v", created)
	}
	if len(fake.created) != 1 || fake.created[0] != "flowpilot-comfyui-draw-01" {
		t.Fatalf("unexpected container name: %v", fake.created)
	}
	hostConfig, ok := fake.lastConfig["HostConfig"].(map[string]any)
	if !ok {
		t.Fatalf("missing host config: %+v", fake.lastConfig)
	}
	requests, ok := hostConfig["DeviceRequests"].([]map[string]any)
	if !ok || len(requests) != 1 {
		t.Fatalf("expected one GPU device request: %+v", hostConfig)
	}
	if _, ok := hostConfig["RestartPolicy"].(map[string]string); !ok {
		t.Fatal("autoStart should set restart policy")
	}
}

func TestCreateWithoutGPUHasNoDeviceRequest(t *testing.T) {
	fake := &fakeDocker{images: map[string]bool{"comfyui:test": true}}
	manager := newManager(fake, 1)
	spec := validSpec()
	spec.GPU = -1
	if _, err := manager.Create(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	hostConfig := fake.lastConfig["HostConfig"].(map[string]any)
	if _, exists := hostConfig["DeviceRequests"]; exists {
		t.Fatal("gpu -1 must not add device requests")
	}
}

func TestCreateValidatesInput(t *testing.T) {
	fake := &fakeDocker{images: map[string]bool{"comfyui:test": true}}
	manager := newManager(fake, 1)
	cases := []struct {
		name string
		mut  func(*Spec)
	}{
		{"中文名称", func(s *Spec) { s.Name = "生产绘图-01" }},
		{"非法端口", func(s *Spec) { s.Port = 80 }},
		{"GPU 越界", func(s *Spec) { s.GPU = 3 }},
		{"镜像缺失", func(s *Spec) { s.Image = "comfyui:missing" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			spec := validSpec()
			testCase.mut(&spec)
			if _, err := manager.Create(context.Background(), spec); err == nil {
				t.Fatal("expected validation error")
			} else {
				var validationError *ValidationError
				if !errors.As(err, &validationError) {
					t.Fatalf("expected ValidationError, got %T: %v", err, err)
				}
			}
		})
	}
	if fake.createCounter != 0 {
		t.Fatal("invalid specs must not reach Docker")
	}
}

func TestCreateRejectsConflicts(t *testing.T) {
	fake := &fakeDocker{images: map[string]bool{"comfyui:test": true}}
	manager := newManager(fake, 1)
	if _, err := manager.Create(context.Background(), validSpec()); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(context.Background(), validSpec()); err == nil {
		t.Fatal("expected duplicate name error")
	}
	other := validSpec()
	other.Name = "draw-02"
	if _, err := manager.Create(context.Background(), other); err == nil {
		t.Fatal("expected duplicate port error")
	}
}

func TestCreateRollsBackOnStartFailure(t *testing.T) {
	fake := &fakeDocker{images: map[string]bool{"comfyui:test": true}, failStart: true}
	manager := newManager(fake, 1)
	if _, err := manager.Create(context.Background(), validSpec()); err == nil {
		t.Fatal("expected start failure")
	}
	if len(fake.removed) != 1 {
		t.Fatal("failed start must remove the created container")
	}
}

func TestActionsAndDelete(t *testing.T) {
	fake := &fakeDocker{images: map[string]bool{"comfyui:test": true}}
	manager := newManager(fake, 1)
	if _, err := manager.Create(context.Background(), validSpec()); err != nil {
		t.Fatal(err)
	}

	stopped, err := manager.Action(context.Background(), "draw-01", "stop")
	if err != nil || stopped.Status != "stopped" {
		t.Fatalf("stop failed: %v %+v", err, stopped)
	}
	started, err := manager.Action(context.Background(), "draw-01", "start")
	if err != nil || started.Status != "running" {
		t.Fatalf("start failed: %v %+v", err, started)
	}
	if _, err := manager.Action(context.Background(), "draw-01", "restart"); err != nil {
		t.Fatal(err)
	}
	if len(fake.stopped) != 2 || len(fake.started) != 3 {
		t.Fatalf("unexpected action counts: stopped=%v started=%v", fake.stopped, fake.started)
	}

	if err := manager.Delete(context.Background(), "draw-01", false, false); err == nil {
		t.Fatal("deleting a running instance without force must fail")
	}
	if err := manager.Delete(context.Background(), "draw-01", true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Get(context.Background(), "draw-01"); err == nil {
		t.Fatal("instance should be gone after delete")
	}
	if _, err := manager.Action(context.Background(), "ghost", "stop"); err == nil {
		t.Fatal("expected not-found error for missing instance")
	}
}

// 停止的实例在 GPU 已被其他运行实例占用时不得启动（创建时校验不够，
// 实例停止后 GPU 可能被新实例占用）。
func TestStartRejectsGPUConflict(t *testing.T) {
	fake := &fakeDocker{images: map[string]bool{"comfyui:test": true}}
	manager := newManager(fake, 1)
	if _, err := manager.Create(context.Background(), validSpec()); err != nil {
		t.Fatal(err)
	}
	// 模拟第二个实例：创建时 GPU 空闲，之后停止，GPU 被 draw-01 占用。
	fake.containers = append(fake.containers, docker.Container{
		ID:    "cdcdcdcdcdcd",
		Names: []string{"/flowpilot-comfyui-draw-02"},
		Image: "comfyui:test",
		State: "exited",
		Labels: map[string]string{
			LabelInstance: "true",
			labelName:     "draw-02",
			labelGPU:      "0",
			labelPort:     "18189",
		},
		Created: time.Now().Unix(),
	})

	_, err := manager.Action(context.Background(), "draw-02", "start")
	if err == nil {
		t.Fatal("GPU 被占用时启动应被拒绝")
	}
	var validationError *ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
	if len(fake.started) != 1 {
		t.Fatalf("冲突实例不应被启动: %v", fake.started)
	}

	// GPU 空闲的实例不受影响。
	fake.containers[1].Labels[labelGPU] = "-1"
	if _, err := manager.Action(context.Background(), "draw-02", "start"); err != nil {
		t.Fatalf("仅 CPU 实例应可启动: %v", err)
	}
}

// 模型仓库必须以只读方式挂载进实例（PRD 10.1），防止容器内误删共享模型。
func TestModelsMountIsReadOnly(t *testing.T) {
	fake := &fakeDocker{images: map[string]bool{"comfyui:test": true}}
	manager := newManager(fake, 1)
	if _, err := manager.Create(context.Background(), validSpec()); err != nil {
		t.Fatal(err)
	}
	hostConfig := fake.lastConfig["HostConfig"].(map[string]any)
	binds, ok := hostConfig["Binds"].([]string)
	if !ok || len(binds) != 3 {
		t.Fatalf("unexpected binds: %+v", hostConfig)
	}
	if !strings.HasSuffix(binds[0], ":ro") {
		t.Fatalf("models bind must be read-only: %s", binds[0])
	}
	for _, bind := range binds[1:] {
		if strings.HasSuffix(bind, ":ro") {
			t.Fatalf("workflows/outputs must stay writable: %s", bind)
		}
	}
}

func TestListIgnoresUnmanagedContainers(t *testing.T) {
	fake := &fakeDocker{
		images: map[string]bool{"comfyui:test": true},
		containers: []docker.Container{
			{ID: "external", Names: []string{"/comfyui-h3"}, State: "running"},
		},
	}
	manager := newManager(fake, 1)
	instances, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 0 {
		t.Fatalf("unmanaged containers must be ignored: %+v", instances)
	}
}

func TestCreateAddsNetworkStorageMounts(t *testing.T) {
	fake := &fakeDocker{images: map[string]bool{"comfyui:test": true}}
	manager := NewManager(fake, func() int { return 1 }, t.TempDir(), "comfyui:test", 8188, "", "", "")
	var yamlBase string
	manager.SetNetworkStorage("/host/shared/network",
		func() bool { return true },
		func(name, basePath string) (string, error) {
			yamlBase = basePath
			return "/host/shared/network-extra/" + name + ".yaml", nil
		},
		func(string) {},
	)
	if _, err := manager.Create(context.Background(), Spec{Name: "net-01", Image: "comfyui:test", GPU: -1, Port: 18200}); err != nil {
		t.Fatal(err)
	}
	hostConfig, ok := fake.lastConfig["HostConfig"].(map[string]any)
	if !ok {
		t.Fatal("缺少 HostConfig")
	}
	binds, ok := hostConfig["Binds"].([]string)
	if !ok {
		t.Fatal("缺少 Binds")
	}
	joined := strings.Join(binds, "\n")
	if yamlBase != "/data" {
		t.Fatalf("basePath 应为 modelsTarget 的父目录 /data，实际 %s", yamlBase)
	}
	if !strings.Contains(joined, "/host/shared/network:/data/models-net:ro") {
		t.Fatalf("缺少网络仓库挂载：%s", joined)
	}
	if !strings.Contains(joined, "/host/shared/network-extra/net-01.yaml:/data/extra_model_paths.yaml:ro") {
		t.Fatalf("缺少 extra_model_paths 挂载：%s", joined)
	}
}

func TestCreateWithoutNetworkStorageKeepsThreeMounts(t *testing.T) {
	fake := &fakeDocker{images: map[string]bool{"comfyui:test": true}}
	manager := NewManager(fake, func() int { return 1 }, t.TempDir(), "comfyui:test", 8188, "", "", "")
	manager.SetNetworkStorage("/host/shared/network", func() bool { return false }, nil, nil)
	if _, err := manager.Create(context.Background(), Spec{Name: "plain-01", Image: "comfyui:test", GPU: -1, Port: 18201}); err != nil {
		t.Fatal(err)
	}
	binds := fake.lastConfig["HostConfig"].(map[string]any)["Binds"].([]string)
	if len(binds) != 3 {
		t.Fatalf("未启用网络存储时应保持 3 个挂载，实际 %d", len(binds))
	}
}

func TestWorkspaceLifecycle(t *testing.T) {
	dockerClient := &fakeDocker{images: map[string]bool{"img:1": true}}
	hostShared := t.TempDir()
	dataDir := t.TempDir()
	manager := NewManager(dockerClient, func() int { return 1 }, hostShared, "img:1", 8188, "/data/models", "/data/outputs", "/data/workflows")
	manager.SetModelLinks(filepath.Join(hostShared, "instance-links"), filepath.Join(dataDir, "instance-links"), filepath.Join(dataDir, "models"))
	manager.SetWorkspaceRoot(filepath.Join(hostShared, "instances"), filepath.Join(dataDir, "instances"))

	spec := Spec{Name: "iso-01", Image: "img:1", GPU: -1, Port: 18100, ContainerPort: 8188, AutoStart: false}
	created, err := manager.Create(context.Background(), spec)
	if err != nil {
		t.Fatalf("创建失败：%v", err)
	}
	if created.UUID == "" || created.Workspace == "" {
		t.Fatalf("新实例应有 UUID 与 workspace：%+v", created)
	}
	for _, sub := range []string{"outputs", "workflows", "user", "config"} {
		if _, err := os.Stat(filepath.Join(dataDir, "instances", created.UUID, sub)); err != nil {
			t.Fatalf("workspace 子目录 %s 未创建：%v", sub, err)
		}
	}
	binds := dockerClient.lastConfig["HostConfig"].(map[string]any)["Binds"].([]string)
	joined := strings.Join(binds, "\n")
	if !strings.Contains(joined, "instances/"+created.UUID+"/outputs") || !strings.Contains(joined, "models:/data/models:ro") {
		t.Fatalf("挂载异常：%v", binds)
	}

	// 默认删除保留 workspace，链接目录清理。
	if err := manager.Delete(context.Background(), "iso-01", true, false); err != nil {
		t.Fatalf("删除失败：%v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "instances", created.UUID, "outputs")); err != nil {
		t.Fatal("默认删除必须保留 workspace 数据")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "instance-links", created.UUID)); !os.IsNotExist(err) {
		t.Fatal("链接目录应随删除清理")
	}

	// 重建复用孤儿 workspace。
	manager.SetWorkspaceReuse(func(name string) string {
		if name == "iso-01" {
			return created.UUID
		}
		return ""
	})
	dockerClient.containers = nil
	rebuilt, err := manager.Create(context.Background(), spec)
	if err != nil {
		t.Fatalf("重建失败：%v", err)
	}
	if rebuilt.UUID != created.UUID {
		t.Fatalf("重建应沿用 UUID：%s → %s", created.UUID, rebuilt.UUID)
	}

	// 彻底删除。
	if err := manager.Delete(context.Background(), "iso-01", true, true); err != nil {
		t.Fatalf("彻底删除失败：%v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "instances", created.UUID)); !os.IsNotExist(err) {
		t.Fatal("purge 应删除 workspace 全部数据")
	}
}
