package image

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	images    []docker.Image
	imageIDs  map[string]string
	removed   []string
	mu        sync.Mutex
}

func (f *fakeImages) ImageExists(_ context.Context, _ string) (bool, error) {
	return f.exists, nil
}

func (f *fakeImages) PullImage(ctx context.Context, _ string, onEvent func(docker.PullEvent)) error {
	if f.onPull != nil {
		f.onPull()
	}
	if f.pullWait != nil {
		select {
		case <-f.pullWait:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	onEvent(docker.PullEvent{Status: "Downloading", Progress: "50%"})
	return f.pullErr
}

func (f *fakeImages) BuildImage(ctx context.Context, reader io.Reader, _ docker.BuildOptions, onEvent func(docker.BuildEvent)) error {
	body, _ := io.ReadAll(reader)
	if !strings.Contains(string(body), "Dockerfile") {
		return errors.New("missing Dockerfile")
	}
	if f.buildWait != nil {
		select {
		case <-f.buildWait:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	onEvent(docker.BuildEvent{Stream: "Step 1/3 : FROM ubuntu\n"})
	onEvent(docker.BuildEvent{Stream: "Successfully tagged flowpilot/comfyui:test\n"})
	return f.buildErr
}

func (f *fakeImages) Images(_ context.Context) ([]docker.Image, error) {
	return f.images, nil
}

func (f *fakeImages) ImageID(_ context.Context, name string) (string, error) {
	if f.imageIDs == nil {
		return "", nil
	}
	return f.imageIDs[name], nil
}

func (f *fakeImages) RemoveImage(_ context.Context, image string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, image)
	return nil
}

func newTestManager(fake *fakeImages) *Manager {
	manager := NewManager(fake, "")
	// 测试环境不做磁盘水位拦截。
	manager.diskFree = func(string) (uint64, error) { return 100 << 30, nil }
	return manager
}

func waitForTaskState(t *testing.T, manager *Manager, id, want string) Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, err := manager.Get(id)
		if err == nil && task.State == want {
			return task
		}
		time.Sleep(5 * time.Millisecond)
	}
	current, _ := manager.Get(id)
	t.Fatalf("任务 %s 状态未变为 %s，当前：%+v", id, want, current)
	return Task{}
}

func TestExistsValidatesImageName(t *testing.T) {
	manager := newTestManager(&fakeImages{exists: true})
	if _, err := manager.Exists(context.Background(), "bad image"); err == nil {
		t.Fatal("含空格的镜像名应被拒绝")
	}
	exists, err := manager.Exists(context.Background(), "comfyui:latest")
	if err != nil || !exists {
		t.Fatalf("应返回存在：%v %v", exists, err)
	}
}

func TestPullLifecycle(t *testing.T) {
	release := make(chan struct{})
	manager := newTestManager(&fakeImages{pullWait: release})

	task, err := manager.SubmitPull("comfyui:latest")
	if err != nil {
		t.Fatal(err)
	}
	// 同名镜像的活动任务重复提交应返回冲突。
	var conflict *ConflictError
	if _, err := manager.SubmitPull("comfyui:latest"); !errors.As(err, &conflict) {
		t.Fatalf("重复拉取应返回 ConflictError：%v", err)
	}
	close(release)
	done := waitForTaskState(t, manager, task.ID, TaskDone)
	if done.Image != "comfyui:latest" {
		t.Fatalf("镜像名错误：%s", done.Image)
	}
	// 完成后可以再次发起。
	again, err := manager.SubmitPull("comfyui:latest")
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskState(t, manager, again.ID, TaskDone)
}

func TestPullFailureRecorded(t *testing.T) {
	manager := newTestManager(&fakeImages{pullErr: errors.New("manifest unknown")})
	task, err := manager.SubmitPull("missing:latest")
	if err != nil {
		t.Fatal(err)
	}
	failed := waitForTaskState(t, manager, task.ID, TaskError)
	if failed.Message != "manifest unknown" {
		t.Fatalf("错误信息未记录：%s", failed.Message)
	}
}

func TestBuildLifecycleAndLogs(t *testing.T) {
	release := make(chan struct{})
	manager := newTestManager(&fakeImages{buildWait: release})
	params := BuildParams{Source: testSource(), Variant: "nvidia-cu130", ImageName: "flowpilot/comfyui:test", ComfyUIRef: "master", PipIndexURL: "tsinghua", WithManager: "1"}
	task, err := manager.SubmitBuild(params)
	if err != nil {
		t.Fatal(err)
	}
	var conflict *ConflictError
	params.ImageName = "flowpilot/comfyui:test"
	if _, err := manager.SubmitBuild(params); !errors.As(err, &conflict) {
		t.Fatalf("同名镜像重复构建应返回 ConflictError：%v", err)
	}
	close(release)
	done := waitForTaskState(t, manager, task.ID, TaskDone)
	if done.Image != "flowpilot/comfyui:test" || done.Variant != "nvidia-cu130" {
		t.Fatalf("构建任务元数据错误：%+v", done)
	}
	if len(done.Logs) < 2 || !strings.Contains(strings.Join(done.Logs, "\n"), "Successfully tagged") {
		t.Fatalf("构建日志未记录：%+v", done.Logs)
	}
}

func TestBuildFailureRecorded(t *testing.T) {
	manager := newTestManager(&fakeImages{buildErr: errors.New("build failed")})
	task, err := manager.SubmitBuild(BuildParams{Source: testSource(), Variant: "cpu", ImageName: "flowpilot/comfyui:cpu-test"})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitForTaskState(t, manager, task.ID, TaskError)
	if failed.Message != "build failed" {
		t.Fatalf("构建错误信息未记录：%s", failed.Message)
	}
	// 失败记录可删除。
	if err := manager.Delete(task.ID); err != nil {
		t.Fatalf("失败任务应可删除：%v", err)
	}
}

// 有界并发：前两个构建并行运行，第三个排队；完成一个后排队任务启动。
func TestBuildConcurrencyLimitAndQueue(t *testing.T) {
	release := make(chan struct{})
	manager := newTestManager(&fakeImages{buildWait: release})
	var ids []string
	for _, name := range []string{"flowpilot/comfyui:a", "flowpilot/comfyui:b", "flowpilot/comfyui:c"} {
		task, err := manager.SubmitBuild(BuildParams{Source: testSource(), Variant: "cpu", ImageName: name})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, task.ID)
	}
	first, _ := manager.Get(ids[0])
	second, _ := manager.Get(ids[1])
	third, _ := manager.Get(ids[2])
	if first.State != TaskRunning || second.State != TaskRunning {
		t.Fatalf("前两个构建应立即运行：%s / %s", first.State, second.State)
	}
	if third.State != TaskQueued {
		t.Fatalf("第三个构建应排队，当前：%s", third.State)
	}
	// 排队中的任务可直接取消。
	if _, err := manager.Cancel(ids[2]); err != nil {
		t.Fatal(err)
	}
	cancelled, _ := manager.Get(ids[2])
	if cancelled.State != TaskCancelled {
		t.Fatalf("排队任务取消后状态错误：%s", cancelled.State)
	}
	close(release)
	waitForTaskState(t, manager, ids[0], TaskDone)
	waitForTaskState(t, manager, ids[1], TaskDone)
}

// 进行中的任务可通过取消 context 中止，状态变为已取消。
func TestCancelRunningBuild(t *testing.T) {
	manager := newTestManager(&fakeImages{buildWait: make(chan struct{})})
	task, err := manager.SubmitBuild(BuildParams{Source: testSource(), Variant: "cpu", ImageName: "flowpilot/comfyui:cancel"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Cancel(task.ID); err != nil {
		t.Fatal(err)
	}
	cancelled := waitForTaskState(t, manager, task.ID, TaskCancelled)
	if cancelled.Message != "已手动停止" {
		t.Fatalf("取消信息错误：%s", cancelled.Message)
	}
}

// 已结束的记录才可删除；进行中删除应报校验错误。
func TestDeleteRunningTaskRejected(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	manager := newTestManager(&fakeImages{buildWait: release})
	task, err := manager.SubmitBuild(BuildParams{Source: testSource(), Variant: "cpu", ImageName: "flowpilot/comfyui:busy"})
	if err != nil {
		t.Fatal(err)
	}
	var validation *ValidationError
	if err := manager.Delete(task.ID); !errors.As(err, &validation) {
		t.Fatalf("进行中任务删除应返回 ValidationError：%v", err)
	}
}

// 构建成功覆盖同名镜像时，被替换的悬挂旧镜像自动清理。
func TestReplacedDanglingImageCleaned(t *testing.T) {
	fake := &fakeImages{
		imageIDs: map[string]string{"flowpilot/comfyui:test": "sha256:old"},
		images:   []docker.Image{{ID: "sha256:old", RepoTags: []string{"<none>:<none>"}}},
	}
	manager := newTestManager(fake)
	task, err := manager.SubmitBuild(BuildParams{Source: testSource(), Variant: "cpu", ImageName: "flowpilot/comfyui:test"})
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskState(t, manager, task.ID, TaskDone)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fake.mu.Lock()
		removed := append([]string{}, fake.removed...)
		fake.mu.Unlock()
		if len(removed) > 0 {
			if removed[0] != "sha256:old" {
				t.Fatalf("清理的镜像 ID 错误：%s", removed[0])
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("被替换的悬挂旧镜像未被自动清理")
}

// 持久化：重启后未完成任务标记为已中断。
func TestInterruptedOnRestart(t *testing.T) {
	store := filepath.Join(t.TempDir(), "image-tasks.json")
	release := make(chan struct{})
	first := NewManager(&fakeImages{buildWait: release}, store)
	first.diskFree = func(string) (uint64, error) { return 100 << 30, nil }
	task, err := first.SubmitBuild(BuildParams{Source: testSource(), Variant: "cpu", ImageName: "flowpilot/comfyui:restart"})
	if err != nil {
		t.Fatal(err)
	}
	// SubmitBuild 同步落盘 running 状态；快照该文件模拟崩溃前的任务记录。
	snapshot, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	store2 := filepath.Join(t.TempDir(), "image-tasks.json")
	if err := os.WriteFile(store2, snapshot, 0o600); err != nil {
		t.Fatal(err)
	}
	// 放行构建协程并等待收口，避免与临时目录清理竞争。
	close(release)
	waitForTaskState(t, first, task.ID, TaskDone)

	// 模拟服务重启：从快照加载，未完成任务应标记为已中断。
	second := NewManager(&fakeImages{}, store2)
	loaded, err := second.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != TaskInterrupted {
		t.Fatalf("重启后未完成任务应标记为已中断，当前：%s", loaded.State)
	}
}

// 磁盘水位不足时构建提交被拒绝。
func TestBuildRejectedWhenDiskLow(t *testing.T) {
	manager := NewManager(&fakeImages{}, "")
	manager.diskFree = func(string) (uint64, error) { return 5 << 30, nil }
	var validation *ValidationError
	if _, err := manager.SubmitBuild(BuildParams{Source: testSource(), Variant: "cpu", ImageName: "flowpilot/comfyui:lowdisk"}); !errors.As(err, &validation) {
		t.Fatalf("磁盘不足应返回 ValidationError：%v", err)
	}
}

func TestBuildDockerfileLabelContinuation(t *testing.T) {
	params := BuildParams{Source: testSource(), Variant: "nvidia-cu128", ImageName: "flowpilot/comfyui:cu128", ComfyUIRef: "master", PipIndexURL: "tsinghua", WithManager: "1", Platform: "linux/amd64"}
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
	script, err := BuildScript(BuildParams{Source: testSource(), Variant: "nvidia-cu128", ImageName: "flowpilot/comfyui:cu128", ComfyUIRef: "master", PipIndexURL: "tsinghua", WithManager: "1", Platform: "linux/amd64"})
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

func testSource() *ComfyVersion {
	commit := "0228a9d73cd7c2daf8f61fe13d56b12efe307171"
	return &ComfyVersion{
		Ref: "master", Kind: "branch", Version: "", Commit: commit,
		CommitShort: commit[:7], Repository: ComfyRepository,
		CommitURL:    ComfyRepositoryURL + "/commit/" + commit,
		DisplayLabel: "master@" + commit[:7],
	}
}
