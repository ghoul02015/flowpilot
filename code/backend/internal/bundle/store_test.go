package bundle

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ghoul02015/flowpilot/internal/database"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "bundle.db"))
	if err != nil {
		t.Fatalf("打开数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := database.Migrate(db, database.Migrations); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	return NewStore(db)
}

func samplePlan(t *testing.T) Plan {
	t.Helper()
	plan, err := Expand(mustDefault(t), "h3-image-base", "default", nil)
	if err != nil {
		t.Fatalf("展开失败：%v", err)
	}
	return plan
}

func TestCreateAndGetInstall(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	plan := samplePlan(t)
	install := Install{
		ID: "install-test-1", BundleID: plan.BundleID, BundleVersion: plan.BundleVersion,
		ManifestChecksum: plan.ManifestChecksum, Variant: plan.Variant, SelectedOptions: plan.SelectedOptions,
		TargetRepository: "models", Status: StateInstalling, TotalBytes: plan.TotalBytes,
		RequestKey: "req-1", Operator: "admin",
	}
	if err := store.CreateInstall(ctx, install, plan); err != nil {
		t.Fatalf("创建计划失败：%v", err)
	}
	loaded, err := store.GetInstall(ctx, install.ID)
	if err != nil {
		t.Fatalf("读取计划失败：%v", err)
	}
	if len(loaded.Items) != len(plan.Items) {
		t.Fatalf("子项数量不一致：%d vs %d", len(loaded.Items), len(plan.Items))
	}
	if loaded.ManifestChecksum != plan.ManifestChecksum || loaded.TotalBytes != plan.TotalBytes {
		t.Fatal("计划快照未完整持久化")
	}
	if loaded.Items[0].ExpectedSHA256 == "" || loaded.Items[0].Status != ItemMissing {
		t.Fatalf("子项初始状态异常：%+v", loaded.Items[0])
	}
}

func TestActiveInstallUnique(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	plan := samplePlan(t)
	base := Install{
		BundleID: plan.BundleID, BundleVersion: plan.BundleVersion, ManifestChecksum: plan.ManifestChecksum,
		Variant: plan.Variant, TargetRepository: "models", Status: StateInstalling, TotalBytes: plan.TotalBytes,
		Operator: "admin",
	}
	first := base
	first.ID = "install-a"
	if err := store.CreateInstall(ctx, first, plan); err != nil {
		t.Fatalf("首个计划失败：%v", err)
	}
	second := base
	second.ID = "install-b"
	if err := store.CreateInstall(ctx, second, plan); !errors.Is(err, ErrActiveInstall) {
		t.Fatalf("应拒绝第二个活动计划，实际：%v", err)
	}
	// 终态后允许再次创建。
	if err := store.UpdateInstall(ctx, first.ID, StateFilesReady, "", plan.TotalBytes, "", ""); err != nil {
		t.Fatal(err)
	}
	third := base
	third.ID = "install-c"
	if err := store.CreateInstall(ctx, third, plan); err != nil {
		t.Fatalf("终态后应允许新计划：%v", err)
	}
}

func TestRequestKeyIdempotency(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	plan := samplePlan(t)
	install := Install{
		ID: "install-req", BundleID: plan.BundleID, BundleVersion: plan.BundleVersion,
		ManifestChecksum: plan.ManifestChecksum, Variant: plan.Variant, TargetRepository: "models",
		Status: StateInstalling, TotalBytes: plan.TotalBytes, RequestKey: "same-key", Operator: "admin",
	}
	if err := store.CreateInstall(ctx, install, plan); err != nil {
		t.Fatalf("创建失败：%v", err)
	}
	found, ok, err := store.FindByRequestKey(ctx, "same-key")
	if err != nil || !ok || found.ID != install.ID {
		t.Fatalf("幂等查询失败：ok=%v err=%v", ok, err)
	}
	if _, ok, _ := store.FindByRequestKey(ctx, "missing"); ok {
		t.Fatal("不存在的 requestKey 不应命中")
	}
}

func TestMarkInterrupted(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	plan := samplePlan(t)
	install := Install{
		ID: "install-int", BundleID: plan.BundleID, BundleVersion: plan.BundleVersion,
		ManifestChecksum: plan.ManifestChecksum, Variant: plan.Variant, TargetRepository: "models",
		Status: StateInstalling, TotalBytes: plan.TotalBytes, Operator: "admin",
	}
	if err := store.CreateInstall(ctx, install, plan); err != nil {
		t.Fatal(err)
	}
	affected, err := store.MarkInterrupted(ctx)
	if err != nil || affected != 1 {
		t.Fatalf("应中断 1 个计划：%d err=%v", affected, err)
	}
	loaded, _ := store.GetInstall(ctx, install.ID)
	if loaded.Status != StatePaused || loaded.RecoveryReason == "" {
		t.Fatalf("重启后应标记待继续：%+v", loaded.Status)
	}
}

func TestLicenseAndAcceptance(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	confirmation := LicenseConfirmation{
		ID: "lic-1", Org: "default", ModelName: "MiniMax-H3-Image", LicenseVersion: "2026-11-10",
		LicenseName: "MiniMax-H3-Image Community License Agreement",
		OfficialURL: "https://github.com/MiniMax-AI/MiniMax-H3-Image/blob/main/LICENSE",
		BundleID:    "h3-image-base", BundleVersion: "1.0.0",
		DeliveryMethod: "online", Operator: "admin", Environment: "test-host", ConfirmedAt: time.Now().UTC(),
	}
	if err := store.ConfirmLicense(ctx, confirmation); err != nil {
		t.Fatal(err)
	}
	ok, err := store.LicenseConfirmed(ctx, "default", "MiniMax-H3-Image", "2026-11-10")
	if err != nil || !ok {
		t.Fatalf("许可确认应命中：%v", err)
	}
	// 按模型判定：S1 确认后，同模型的 S2 不再重复确认。
	if ok, _ := store.LicenseConfirmed(ctx, "default", "MiniMax-H3-Image", "2026-11-10"); !ok {
		t.Fatal("同模型同版本应视为已确认")
	}
	// 版本变化必须重新确认。
	if ok, _ := store.LicenseConfirmed(ctx, "default", "MiniMax-H3-Image", "2026-11-11"); ok {
		t.Fatal("许可版本变化后不应复用旧确认")
	}
	// 不同模型不共用确认：H3-Image 的确认不能覆盖 H3。
	if ok, _ := store.LicenseConfirmed(ctx, "default", "MiniMax-H3", "2026-11-10"); ok {
		t.Fatal("不同模型不应共用确认记录")
	}
	// 重复确认同一版本保持幂等且只保留一条。
	if err := store.ConfirmLicense(ctx, confirmation); err != nil {
		t.Fatal(err)
	}
	history, err := store.ListLicenseConfirmations(ctx, "default", "MiniMax-H3-Image", 10)
	if err != nil || len(history) != 1 {
		t.Fatalf("同一模型同一版本只应有一条记录：len=%d err=%v", len(history), err)
	}
	if history[0].Operator != "admin" || history[0].LicenseVersion != "2026-11-10" {
		t.Fatalf("确认记录字段异常：%+v", history[0])
	}

	for index := 0; index < 3; index++ {
		acceptance := Acceptance{
			ID:       "acc-" + string(rune('a'+index)),
			BundleID: "h3-image-base", BundleVersion: "1.0.0", InstallID: "install-x",
			InstanceName: "comfy-01", Result: "pass",
			Preset:      json.RawMessage(`{"preset":"4MP-T2I"}`),
			Environment: json.RawMessage(`{"gpu":"0:GB10"}`),
			Metrics:     json.RawMessage(`{"firstRunMs":null}`),
			Operator:    "admin", CreatedAt: time.Now().UTC().Add(time.Duration(index) * time.Minute),
		}
		if err := store.CreateAcceptance(ctx, acceptance); err != nil {
			t.Fatal(err)
		}
	}
	list, total, err := store.ListAcceptances(ctx, "h3-image-base", 2, 0)
	if err != nil || total != 3 || len(list) != 2 {
		t.Fatalf("分页异常：total=%d len=%d err=%v", total, len(list), err)
	}
	if list[0].CreatedAt.Before(list[1].CreatedAt) {
		t.Fatal("验收记录应按时间倒序")
	}
}

// TestServiceInstallProgressIncludesCurrentItem 验证进行中文件的实时字节计入进度视图，
// 避免大文件下载期间进度条停在 0%。
func TestServiceInstallProgressIncludesCurrentItem(t *testing.T) {
	store := openStore(t)
	manifest := mustDefault(t)
	plan := samplePlan(t)
	install := Install{
		ID: "install-progress", BundleID: plan.BundleID, BundleVersion: plan.BundleVersion,
		ManifestChecksum: plan.ManifestChecksum, Variant: plan.Variant, TargetRepository: "models",
		Status: StateInstalling, TotalBytes: plan.TotalBytes, Operator: "admin",
	}
	if err := store.CreateInstall(context.Background(), install, plan); err != nil {
		t.Fatal(err)
	}
	// 模拟当前项正在下载，子任务已下载 12 MiB。
	first := plan.Items[0]
	if err := store.UpdateItem(context.Background(), install.ID, first.Key, ItemDownloading, "dl-progress", 0, "", "", "", false); err != nil {
		t.Fatal(err)
	}
	downloader := &progressDownloader{downloaded: 12 << 20}
	service := &Service{
		Manifest: manifest, Store: store,
		Installer: NewInstaller(store, manifest, downloader, func() (RepoSnapshot, int64, error) {
			return NewRepoSnapshot("models", nil), 1 << 40, nil
		}),
	}
	view, err := service.GetInstall(context.Background(), install.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.CurrentItemDownloaded != 12<<20 {
		t.Fatalf("应补充当前文件字节，实际 %d", view.CurrentItemDownloaded)
	}
	if view.CompletedBytes < 12<<20 {
		t.Fatalf("进度应包含当前文件已下载字节，实际 %d", view.CompletedBytes)
	}
	// 落库值不应被视图层污染。
	stored, _ := store.GetInstall(context.Background(), install.ID)
	if stored.CompletedBytes != 0 {
		t.Fatalf("落库的 completedBytes 应仍为 0，实际 %d", stored.CompletedBytes)
	}
}

// progressDownloader 只用于进度视图测试。
type progressDownloader struct{ downloaded int64 }

func (p *progressDownloader) Create(context.Context, DownloadRequest) (DownloadState, error) {
	return DownloadState{}, nil
}

func (p *progressDownloader) Get(context.Context, string) (DownloadState, error) {
	return DownloadState{ID: "dl-progress", Status: "running", DownloadedBytes: p.downloaded, TotalBytes: 1 << 30}, nil
}

func (p *progressDownloader) Action(context.Context, string, string) error { return nil }

func (p *progressDownloader) Delete(context.Context, string) error { return nil }

func (p *progressDownloader) FindByTarget(context.Context, string, string) (DownloadState, bool) {
	return DownloadState{}, false
}

// TestCreateInstallSkipsLicenseForUnlicensedModel 未配置许可的模型无需确认即可安装。
func TestCreateInstallSkipsLicenseForUnlicensedModel(t *testing.T) {
	manifest := mustDefault(t)
	delete(manifest.Licenses, "MiniMax-H3")
	reparsed, err := reparseManifest(t, manifest)
	if err != nil {
		t.Fatalf("未配置许可应允许加载：%v", err)
	}
	store := openStore(t)
	installer := NewInstaller(store, reparsed, newFakeDownloader(), func() (RepoSnapshot, int64, error) {
		return NewRepoSnapshot("models", nil), 1 << 40, nil
	})
	installer.SetPollInterval(2 * time.Millisecond)
	service := &Service{
		Manifest: reparsed, Store: store, Installer: installer,
		ScanRepo: func() (RepoSnapshot, int64, error) { return NewRepoSnapshot("models", nil), 1 << 40, nil },
		Hardware: func() HardwareSummary {
			return HardwareSummary{GPUs: []GPUInfo{{Index: 0, Name: "NVIDIA GB10"}}, UnifiedMemory: true}
		},
		ImageMeta: func() (string, string) { return "0.45.0", "0.11.200" },
	}
	if _, err := service.CreateInstall(context.Background(), "h3-av-base", CreateInstallRequest{Variant: "default"}, "admin"); err != nil {
		t.Fatalf("未配置许可的模型应可直接安装：%v", err)
	}
	// 已配置许可且未确认的模型仍然拒绝。
	if _, err := service.CreateInstall(context.Background(), "h3-image-base", CreateInstallRequest{Variant: "default"}, "admin"); !errors.Is(err, ErrLicenseNotConfirmed) {
		t.Fatalf("未确认许可应返回 ErrLicenseNotConfirmed，实际 %v", err)
	}
	// 未配置许可的模型不接受确认记录，避免产生无意义的空版本记录。
	if _, err := service.ConfirmLicense(context.Background(), "h3-av-base", "online", "admin"); err == nil {
		t.Fatal("未配置许可的模型不应允许写入确认记录")
	}
}
