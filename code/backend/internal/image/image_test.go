package image

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ghoul02015/flowpilot/internal/docker"
)

type fakeImages struct {
	exists   bool
	pullErr  error
	onPull   func()
	pullWait chan struct{}
}

func (f *fakeImages) ImageExists(_ context.Context, _ string) (bool, error) {
	return f.exists, nil
}

func (f *fakeImages) PullImage(_ context.Context, _ string, onEvent func(docker.PullEvent)) error {
	if f.onPull != nil {
		f.onPull()
	}
	if f.pullWait != nil {
		<-f.pullWait
	}
	onEvent(docker.PullEvent{Status: "Downloading", Progress: "50%"})
	return f.pullErr
}

func waitForState(t *testing.T, puller *Puller, want string) Status {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if status := puller.Status(); status.State == want {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("状态未变为 %s，当前：%+v", want, puller.Status())
	return Status{}
}

func TestExistsValidatesImageName(t *testing.T) {
	puller := NewPuller(&fakeImages{exists: true})
	if _, err := puller.Exists(context.Background(), "bad image"); err == nil {
		t.Fatal("含空格的镜像名应被拒绝")
	}
	exists, err := puller.Exists(context.Background(), "comfyui:latest")
	if err != nil || !exists {
		t.Fatalf("应返回存在：%v %v", exists, err)
	}
}

func TestPullLifecycle(t *testing.T) {
	release := make(chan struct{})
	puller := NewPuller(&fakeImages{pullWait: release})

	if err := puller.Start("comfyui:latest"); err != nil {
		t.Fatal(err)
	}
	// 进行中的重复提交应返回冲突。
	var conflict *ConflictError
	if err := puller.Start("comfyui:latest"); !errors.As(err, &conflict) {
		t.Fatalf("重复拉取应返回 ConflictError：%v", err)
	}
	close(release)
	status := waitForState(t, puller, "done")
	if status.Image != "comfyui:latest" {
		t.Fatalf("镜像名错误：%s", status.Image)
	}
	// 完成后可以再次发起。
	if err := puller.Start("comfyui:latest"); err != nil {
		t.Fatal(err)
	}
	waitForState(t, puller, "done")
}

func TestPullFailureRecorded(t *testing.T) {
	puller := NewPuller(&fakeImages{pullErr: errors.New("manifest unknown")})
	if err := puller.Start("missing:latest"); err != nil {
		t.Fatal(err)
	}
	status := waitForState(t, puller, "error")
	if status.Message != "manifest unknown" {
		t.Fatalf("错误信息未记录：%s", status.Message)
	}
}
