package image

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ghoul02015/flowpilot/internal/docker"
)

type fakeImages struct {
	exists    bool
	pullErr   error
	buildErr  error
	onPull    func()
	pullWait  chan struct{}
	buildWait chan struct{}
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

func (f *fakeImages) BuildImage(_ context.Context, reader io.Reader, _ docker.BuildOptions, onEvent func(docker.BuildEvent)) error {
	body, _ := io.ReadAll(reader)
	if !strings.Contains(string(body), "Dockerfile") {
		return errors.New("missing Dockerfile")
	}
	if f.buildWait != nil {
		<-f.buildWait
	}
	onEvent(docker.BuildEvent{Stream: "Step 1/3 : FROM ubuntu\n"})
	onEvent(docker.BuildEvent{Stream: "Successfully tagged flowpilot/comfyui:test\n"})
	return f.buildErr
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

func waitForBuildState(t *testing.T, puller *Puller, want string) BuildStatus {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if status := puller.BuildStatus(); status.State == want {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("构建状态未变为 %s，当前：%+v", want, puller.BuildStatus())
	return BuildStatus{}
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

func TestBuildLifecycleAndLogs(t *testing.T) {
	release := make(chan struct{})
	puller := NewPuller(&fakeImages{buildWait: release})
	params := BuildParams{Variant: "nvidia-cu130", ImageName: "flowpilot/comfyui:test", ComfyUIRef: "master", PipIndexURL: "tsinghua", WithManager: "1"}
	if err := puller.StartBuild(params); err != nil {
		t.Fatal(err)
	}
	var conflict *ConflictError
	if err := puller.StartBuild(params); !errors.As(err, &conflict) {
		t.Fatalf("重复构建应返回 ConflictError：%v", err)
	}
	close(release)
	status := waitForBuildState(t, puller, "done")
	if status.Image != "flowpilot/comfyui:test" || status.Variant != "nvidia-cu130" {
		t.Fatalf("构建状态元数据错误：%+v", status)
	}
	if len(status.Logs) < 2 || !strings.Contains(strings.Join(status.Logs, "\n"), "Successfully tagged") {
		t.Fatalf("构建日志未记录：%+v", status.Logs)
	}
}

func TestBuildFailureRecorded(t *testing.T) {
	puller := NewPuller(&fakeImages{buildErr: errors.New("build failed")})
	if err := puller.StartBuild(BuildParams{Variant: "cpu", ImageName: "flowpilot/comfyui:cpu-test"}); err != nil {
		t.Fatal(err)
	}
	status := waitForBuildState(t, puller, "error")
	if status.Message != "build failed" {
		t.Fatalf("构建错误信息未记录：%s", status.Message)
	}
}

func TestBuildDockerfileLabelContinuation(t *testing.T) {
	params := BuildParams{Variant: "nvidia-cu128", ImageName: "flowpilot/comfyui:cu128", ComfyUIRef: "master", PipIndexURL: "tsinghua", WithManager: "1", Platform: "linux/amd64"}
	recipe, err := params.validate()
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := BuildDockerfile(params, recipe)
	if strings.Contains(dockerfile, `\\`) {
		t.Fatalf("Dockerfile LABEL 续行符不应输出双反斜杠：\n%s", dockerfile)
	}
	if !strings.Contains(dockerfile, `flowpilot.build.platform="linux/amd64"`) || !strings.Contains(dockerfile, `flowpilot.build.summary=`) {
		t.Fatalf("Dockerfile 缺少构建参数标签：\n%s", dockerfile)
	}
}

func TestBuildScriptIncludesParsableDockerfileLabel(t *testing.T) {
	script, err := BuildScript(BuildParams{Variant: "nvidia-cu128", ImageName: "flowpilot/comfyui:cu128", ComfyUIRef: "master", PipIndexURL: "tsinghua", WithManager: "1", Platform: "linux/amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(script, `\\`) {
		t.Fatalf("构建脚本内嵌 Dockerfile 不应包含双反斜杠续行：\n%s", script)
	}
	if !strings.Contains(script, `--platform 'linux/amd64'`) {
		t.Fatalf("构建脚本缺少 linux/amd64 platform 参数：\n%s", script)
	}
}
