package instance

import (
	"context"
	"errors"
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
	if decoded, ok := config.(map[string]any); ok {
		f.lastConfig = decoded
	}
	id := strings.Repeat("ab", 6)
	f.containers = append(f.containers, docker.Container{
		ID:    id,
		Names: []string{"/" + name},
		Image: "comfyui:test",
		State: "created",
		Labels: map[string]string{
			LabelInstance:  "true",
			labelName:      strings.TrimPrefix(name, containerPrefix),
			labelGPU:       "0",
			labelPort:      "18188",
			labelAutoStart: "true",
		},
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
	return NewManager(dockerAPI, func() int { return gpuCount }, "/host/data", "comfyui:test", 8188)
}

func validSpec() Spec {
	return Spec{Name: "draw-01", Image: "comfyui:test", GPU: 0, Port: 18188, ContainerPort: 8188, AutoStart: true}
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

	if err := manager.Delete(context.Background(), "draw-01", false); err == nil {
		t.Fatal("deleting a running instance without force must fail")
	}
	if err := manager.Delete(context.Background(), "draw-01", true); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Get(context.Background(), "draw-01"); err == nil {
		t.Fatal("instance should be gone after delete")
	}
	if _, err := manager.Action(context.Background(), "ghost", "stop"); err == nil {
		t.Fatal("expected not-found error for missing instance")
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
