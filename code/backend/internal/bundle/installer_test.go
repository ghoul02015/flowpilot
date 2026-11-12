package bundle

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeTask struct {
	id         string
	fileName   string
	status     string
	downloaded int64
	total      int64
	message    string
}

// fakeDownloader 是确定性的下载引擎替身。
type fakeDownloader struct {
	mu      sync.Mutex
	tasks   map[string]*fakeTask
	seq     int
	failFor map[string]string // 文件名 -> 失败原因
	pending bool              // true 表示任务停在 running，需手动推进
}

func newFakeDownloader() *fakeDownloader {
	return &fakeDownloader{tasks: map[string]*fakeTask{}, failFor: map[string]string{}}
}

func (f *fakeDownloader) Create(_ context.Context, request DownloadRequest) (DownloadState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// 与真实引擎一致：同目标已有任务时拒绝创建。
	for _, task := range f.tasks {
		if task.fileName == request.FileName {
			return DownloadState{}, fmt.Errorf("已有目标为 %s/%s 的任务在进行中", request.Category, request.FileName)
		}
	}
	f.seq++
	id := fmt.Sprintf("dl-%d", f.seq)
	status := "completed"
	message := ""
	if reason, shouldFail := f.failFor[request.FileName]; shouldFail {
		status = "failed"
		message = reason
	}
	if f.pending {
		status = "running"
	}
	f.tasks[id] = &fakeTask{id: id, fileName: request.FileName, status: status, total: 100, downloaded: 100, message: message}
	return DownloadState{ID: id, Status: status, DownloadedBytes: 100, TotalBytes: 100, Message: message}, nil
}

func (f *fakeDownloader) Get(_ context.Context, id string) (DownloadState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task, ok := f.tasks[id]
	if !ok {
		return DownloadState{}, fmt.Errorf("任务不存在：%s", id)
	}
	return DownloadState{ID: task.id, Status: task.status, DownloadedBytes: task.downloaded, TotalBytes: task.total, Message: task.message}, nil
}

func (f *fakeDownloader) Action(_ context.Context, id, action string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	task, ok := f.tasks[id]
	if !ok {
		return fmt.Errorf("任务不存在：%s", id)
	}
	switch action {
	case "pause":
		task.status = "paused"
	case "resume", "retry":
		task.status = "completed"
	case "cancel":
		task.status = "cancelled"
	}
	return nil
}

func (f *fakeDownloader) FindByTarget(_ context.Context, _, fileName string) (DownloadState, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, task := range f.tasks {
		if task.fileName == fileName {
			return DownloadState{ID: task.id, Status: task.status, DownloadedBytes: task.downloaded, TotalBytes: task.total, Message: task.message}, true
		}
	}
	return DownloadState{}, false
}

func (f *fakeDownloader) completeAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, task := range f.tasks {
		if task.status == "running" || task.status == "paused" {
			task.status = "completed"
		}
	}
}

func newTestInstaller(t *testing.T, downloader Downloader, sizes map[string]int64) (*Installer, *Store) {
	t.Helper()
	store := openStore(t)
	manifest := mustDefault(t)
	installer := NewInstaller(store, manifest, downloader, func() (RepoSnapshot, int64, error) {
		return NewRepoSnapshot("models", sizes), 1 << 40, nil
	})
	installer.SetPollInterval(2 * time.Millisecond)
	return installer, store
}

func createInstall(t *testing.T, store *Store, plan Plan, status string) Install {
	t.Helper()
	install := Install{
		ID: "install-" + plan.BundleID + "-" + plan.Variant, BundleID: plan.BundleID,
		BundleVersion: plan.BundleVersion, ManifestChecksum: plan.ManifestChecksum,
		Variant: plan.Variant, SelectedOptions: plan.SelectedOptions,
		TargetRepository: "models", Status: status, TotalBytes: plan.TotalBytes, Operator: "admin",
	}
	if err := store.CreateInstall(context.Background(), install, plan); err != nil {
		t.Fatalf("创建计划失败：%v", err)
	}
	return install
}

func TestInstallerCompletesAllRequiredItems(t *testing.T) {
	manifest := mustDefault(t)
	plan, err := Expand(manifest, "h3-image-base", "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	downloader := newFakeDownloader()
	installer, store := newTestInstaller(t, downloader, nil)
	install := createInstall(t, store, plan, StateQueued)

	if err := installer.Start(context.Background(), install.ID); err != nil {
		t.Fatalf("启动失败：%v", err)
	}
	installer.Wait(install.ID)

	loaded, err := store.GetInstall(context.Background(), install.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != StateFilesReady {
		t.Fatalf("应完成安装，实际 %s（%s）", loaded.Status, loaded.ErrorMessage)
	}
	for _, item := range loaded.Items {
		if item.Status != ItemPresent {
			t.Fatalf("文件项 %s 应为 present，实际 %s", item.ItemKey, item.Status)
		}
	}
	if loaded.CompletedBytes != plan.TotalBytes {
		t.Fatalf("完成字节不符：%d vs %d", loaded.CompletedBytes, plan.TotalBytes)
	}
}

func TestInstallerSkipsExistingFiles(t *testing.T) {
	manifest := mustDefault(t)
	plan, err := Expand(manifest, "h3-image-base", "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	sizes := map[string]int64{}
	for _, item := range plan.Items {
		sizes[item.TargetRelativePath] = item.SizeBytes
	}
	downloader := newFakeDownloader()
	installer, store := newTestInstaller(t, downloader, sizes)
	install := createInstall(t, store, plan, StateQueued)
	if err := installer.Start(context.Background(), install.ID); err != nil {
		t.Fatal(err)
	}
	installer.Wait(install.ID)

	loaded, _ := store.GetInstall(context.Background(), install.ID)
	if loaded.Status != StateFilesReady {
		t.Fatalf("齐备仓库应直接完成，实际 %s", loaded.Status)
	}
	downloader.mu.Lock()
	created := len(downloader.tasks)
	downloader.mu.Unlock()
	if created != 0 {
		t.Fatalf("已存在文件不应创建下载任务，实际创建 %d 个", created)
	}
}

func TestInstallerFailsOnRequiredItem(t *testing.T) {
	manifest := mustDefault(t)
	plan, err := Expand(manifest, "h3-image-base", "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	downloader := newFakeDownloader()
	downloader.failFor[plan.Items[1].TargetRelativePath] = "网络中断"
	// Create 用文件名匹配，需要换成文件名。
	delete(downloader.failFor, plan.Items[1].TargetRelativePath)
	downloader.failFor[fileNameOf(plan.Items[1].TargetRelativePath)] = "网络中断"

	installer, store := newTestInstaller(t, downloader, nil)
	install := createInstall(t, store, plan, StateQueued)
	if err := installer.Start(context.Background(), install.ID); err != nil {
		t.Fatal(err)
	}
	installer.Wait(install.ID)

	loaded, _ := store.GetInstall(context.Background(), install.ID)
	if loaded.Status != StateFailed {
		t.Fatalf("必需项失败应整单失败，实际 %s", loaded.Status)
	}
	if !strings.Contains(loaded.ErrorMessage, "网络中断") {
		t.Fatalf("错误信息应保留原因，实际 %q", loaded.ErrorMessage)
	}
}

func TestInstallerPauseAndResume(t *testing.T) {
	manifest := mustDefault(t)
	plan, err := Expand(manifest, "h3-image-base", "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	downloader := newFakeDownloader()
	downloader.pending = true
	installer, store := newTestInstaller(t, downloader, nil)
	install := createInstall(t, store, plan, StateQueued)

	if err := installer.Start(context.Background(), install.ID); err != nil {
		t.Fatal(err)
	}
	// 等首个子任务创建。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if installer.currentTask(install.ID) != "" {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if err := installer.Pause(context.Background(), install.ID); err != nil {
		t.Fatalf("暂停失败：%v", err)
	}
	loaded, _ := store.GetInstall(context.Background(), install.ID)
	if loaded.Status != StatePaused {
		t.Fatalf("暂停后应为 paused，实际 %s", loaded.Status)
	}

	// 恢复：先让子任务可完成，再继续。
	downloader.pending = false
	downloader.completeAll()
	if err := installer.Start(context.Background(), install.ID); err != nil {
		t.Fatalf("恢复失败：%v", err)
	}
	installer.Wait(install.ID)
	loaded, _ = store.GetInstall(context.Background(), install.ID)
	if loaded.Status != StateFilesReady {
		t.Fatalf("恢复后应完成，实际 %s", loaded.Status)
	}
}

func TestInstallerCancelKeepsCompleted(t *testing.T) {
	manifest := mustDefault(t)
	plan, err := Expand(manifest, "h3-image-base", "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	downloader := newFakeDownloader()
	downloader.pending = true
	installer, store := newTestInstaller(t, downloader, nil)
	install := createInstall(t, store, plan, StateQueued)
	if err := installer.Start(context.Background(), install.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if installer.currentTask(install.ID) != "" {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if err := installer.Cancel(context.Background(), install.ID); err != nil {
		t.Fatalf("取消失败：%v", err)
	}
	loaded, _ := store.GetInstall(context.Background(), install.ID)
	if loaded.Status != StateCancelled {
		t.Fatalf("取消后应为 cancelled，实际 %s", loaded.Status)
	}
	if loaded.FinishedAt == nil {
		t.Fatal("取消应写入结束时间")
	}
}

func TestInstallerRetryFailed(t *testing.T) {
	manifest := mustDefault(t)
	plan, err := Expand(manifest, "h3-image-base", "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	downloader := newFakeDownloader()
	downloader.failFor[fileNameOf(plan.Items[0].TargetRelativePath)] = "临时失败"
	installer, store := newTestInstaller(t, downloader, nil)
	install := createInstall(t, store, plan, StateQueued)
	if err := installer.Start(context.Background(), install.ID); err != nil {
		t.Fatal(err)
	}
	installer.Wait(install.ID)
	loaded, _ := store.GetInstall(context.Background(), install.ID)
	if loaded.Status != StateFailed {
		t.Fatalf("应先失败，实际 %s", loaded.Status)
	}

	// 恢复网络后重试。
	downloader.mu.Lock()
	downloader.failFor = map[string]string{}
	downloader.mu.Unlock()
	if err := installer.RetryFailed(context.Background(), install.ID); err != nil {
		t.Fatalf("重试失败：%v", err)
	}
	installer.Wait(install.ID)
	loaded, _ = store.GetInstall(context.Background(), install.ID)
	if loaded.Status != StateFilesReady {
		t.Fatalf("重试后应完成，实际 %s（%s）", loaded.Status, loaded.ErrorMessage)
	}
	for _, item := range loaded.Items {
		if item.Attempts == 0 {
			t.Fatalf("重试应记录尝试次数：%s", item.ItemKey)
		}
	}
}

func TestInstallerOptionalFailureDoesNotBlock(t *testing.T) {
	manifest := mustDefault(t)
	plan, err := Expand(manifest, "h3-av-base", "turbo-4step", nil)
	if err != nil {
		t.Fatal(err)
	}
	downloader := newFakeDownloader()
	// 让可选 LoRA 失败。
	downloader.failFor["minimax_h3_fl2v_turbo_4step_v1.0_768p_comfyui_bf16.safetensors"] = "可选失败"
	installer, store := newTestInstaller(t, downloader, nil)
	install := createInstall(t, store, plan, StateQueued)
	if err := installer.Start(context.Background(), install.ID); err != nil {
		t.Fatal(err)
	}
	installer.Wait(install.ID)

	loaded, _ := store.GetInstall(context.Background(), install.ID)
	if loaded.Status != StateFilesReady {
		t.Fatalf("可选项失败不应阻断，实际 %s（%s）", loaded.Status, loaded.ErrorMessage)
	}
}

// TestInstallerReusesPausedTask 验证取消或重启后残留的 paused 子任务会被复用续传，
// 而不是因"目标已有任务在进行中"直接失败。
func TestInstallerReusesPausedTask(t *testing.T) {
	manifest := mustDefault(t)
	plan, err := Expand(manifest, "h3-image-base", "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	downloader := newFakeDownloader()
	target := fileNameOf(plan.Items[0].TargetRelativePath)
	// 模拟上次取消／重启留下的暂停任务。
	downloader.tasks["dl-stale"] = &fakeTask{id: "dl-stale", fileName: target, status: "paused", total: 100, downloaded: 40, message: "平台重启，下载已暂停"}

	installer, store := newTestInstaller(t, downloader, nil)
	install := createInstall(t, store, plan, StateQueued)
	if err := installer.Start(context.Background(), install.ID); err != nil {
		t.Fatal(err)
	}
	installer.Wait(install.ID)

	loaded, _ := store.GetInstall(context.Background(), install.ID)
	if loaded.Status != StateFilesReady {
		t.Fatalf("残留任务应被复用并完成，实际 %s（%s）", loaded.Status, loaded.ErrorMessage)
	}
	// 复用任务不应被当成新任务重复创建。
	downloader.mu.Lock()
	taskCount := len(downloader.tasks)
	downloader.mu.Unlock()
	// 3 个文件 = 1 个复用 + 2 个新建。
	if taskCount != 3 {
		t.Fatalf("应复用残留任务并新建 2 个，实际任务数 %d", taskCount)
	}
}
