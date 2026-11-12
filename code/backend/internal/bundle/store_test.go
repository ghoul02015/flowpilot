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
		ID: "lic-1", BundleID: "h3-image-base", BundleVersion: "1.0.0",
		AcknowledgementVersion: "minimax-h3-license-2026-11", LicenseSnapshotDate: "2026-11-12",
		DeliveryMethod: "online", Operator: "admin", ConfirmedAt: time.Now().UTC(),
	}
	if err := store.ConfirmLicense(ctx, confirmation); err != nil {
		t.Fatal(err)
	}
	ok, err := store.LicenseConfirmed(ctx, "h3-image-base", "1.0.0", "minimax-h3-license-2026-11", "admin")
	if err != nil || !ok {
		t.Fatalf("许可确认应命中：%v", err)
	}
	// 旧版本确认不复用。
	if ok, _ := store.LicenseConfirmed(ctx, "h3-image-base", "0.9.0", "minimax-h3-license-2026-11", "admin"); ok {
		t.Fatal("不同 bundleVersion 的确认不应复用")
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
