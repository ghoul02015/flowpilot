package database

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "flowpilot-test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := Migrate(db, Migrations); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	return db
}

func TestMigrateIdempotent(t *testing.T) {
	db := openTestDB(t)
	for i := 0; i < 3; i++ {
		applied, err := Migrate(db, Migrations)
		if err != nil {
			t.Fatalf("第 %d 次迁移失败：%v", i+1, err)
		}
		if applied != 0 {
			t.Fatalf("重复迁移不应再应用任何版本，第 %d 次应用了 %d 个", i+1, applied)
		}
	}
	version, err := SchemaVersion(db)
	if err != nil || version != 6 {
		t.Fatalf("schema 版本应为 6，实际 %d（err=%v）", version, err)
	}
}

func TestMigrateChecksumMismatchRejected(t *testing.T) {
	db := openTestDB(t)
	tampered := []Migration{{
		Version: 1,
		Name:    "core-tables-tampered",
		SQL:     "CREATE TABLE IF NOT EXISTS evil (x TEXT);",
	}}
	if _, err := Migrate(db, tampered); err == nil {
		t.Fatal("已应用迁移内容被篡改时必须报错，不能静默通过")
	}
}

func TestSyncImportUpdateRebuildOrphan(t *testing.T) {
	store := NewStore(openTestDB(t))
	created := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)

	// 1. 导入。
	report := store.Sync([]Snapshot{{
		Name: "alpha", ContainerID: "c1", ContainerName: "flowpilot-comfyui-alpha",
		Image: "img:1", GPU: 0, Port: 8188, AutoStart: true,
		ModelsTarget: "/data/models", OutputsTarget: "/data/outputs", WorkflowsTarget: "/data/workflows",
		ModelLinks: true, Status: "running", CreatedAt: created,
	}})
	if len(report.Imported) != 1 || report.Imported[0] != "alpha" {
		t.Fatalf("应导入 alpha，实际 %+v", report.Imported)
	}
	firstUUID := report.Diffs[0].UUID
	if firstUUID == "" {
		t.Fatal("导入记录应有 UUID")
	}
	total, orphans := store.Counts()
	if total != 1 || orphans != 0 {
		t.Fatalf("导入后应为 1 实例 0 孤儿，实际 %d/%d", total, orphans)
	}

	// 2. 重复同步无变化。
	report = store.Sync([]Snapshot{{
		Name: "alpha", ContainerID: "c1", ContainerName: "flowpilot-comfyui-alpha",
		Image: "img:1", GPU: 0, Port: 8188, AutoStart: true,
		ModelsTarget: "/data/models", OutputsTarget: "/data/outputs", WorkflowsTarget: "/data/workflows",
		ModelLinks: true, Status: "running", CreatedAt: created,
	}})
	if report.Unchanged != 1 || len(report.Updated) != 0 {
		t.Fatalf("重复同步应全部 unchanged，实际 %+v", report)
	}

	// 3. 字段变化 → updated。
	report = store.Sync([]Snapshot{{
		Name: "alpha", ContainerID: "c1", ContainerName: "flowpilot-comfyui-alpha",
		Image: "img:2", GPU: 0, Port: 8189, AutoStart: true,
		ModelsTarget: "/data/models", OutputsTarget: "/data/outputs", WorkflowsTarget: "/data/workflows",
		ModelLinks: true, Status: "running", CreatedAt: created,
	}})
	if len(report.Updated) != 1 {
		t.Fatalf("字段变化应报告 updated，实际 %+v", report)
	}
	diffFields := map[string]bool{}
	for _, d := range report.Diffs[0].Diffs {
		diffFields[d.Field] = true
	}
	if !diffFields["image"] || !diffFields["port"] {
		t.Fatalf("应包含 image/port 差异，实际 %+v", report.Diffs[0].Diffs)
	}

	// 4. 同名新容器 → rebuilt 沿用 UUID。
	report = store.Sync([]Snapshot{{
		Name: "alpha", ContainerID: "c2", ContainerName: "flowpilot-comfyui-alpha",
		Image: "img:2", GPU: 0, Port: 8189, AutoStart: true,
		ModelsTarget: "/data/models", OutputsTarget: "/data/outputs", WorkflowsTarget: "/data/workflows",
		ModelLinks: true, Status: "running", CreatedAt: created,
	}})
	if len(report.Rebuilt) != 1 {
		t.Fatalf("同名新容器应报告 rebuilt，实际 %+v", report)
	}
	if report.Diffs[0].UUID != firstUUID {
		t.Fatalf("重建必须沿用 UUID：原 %s，实际 %s", firstUUID, report.Diffs[0].UUID)
	}

	// 5. 容器消失 → orphan，记录保留。
	report = store.Sync(nil)
	if len(report.Orphans) != 1 || report.Orphans[0] != "alpha" {
		t.Fatalf("容器消失应标记孤儿，实际 %+v", report.Orphans)
	}
	total, orphans = store.Counts()
	if total != 1 || orphans != 1 {
		t.Fatalf("孤儿不删除记录：应为 1 实例 1 孤儿，实际 %d/%d", total, orphans)
	}
	summary, ok := store.LastSummary()
	if !ok || summary.Orphans != 1 {
		t.Fatalf("最近同步摘要应记录孤儿 1，实际 %+v（ok=%v）", summary, ok)
	}

	// 6. 孤儿回归：同 UUID 记录解除孤儿标记。
	report = store.Sync([]Snapshot{{
		Name: "alpha", ContainerID: "c2", ContainerName: "flowpilot-comfyui-alpha",
		Image: "img:2", GPU: 0, Port: 8189, AutoStart: true,
		ModelsTarget: "/data/models", OutputsTarget: "/data/outputs", WorkflowsTarget: "/data/workflows",
		ModelLinks: true, Status: "running", CreatedAt: created,
	}})
	_, orphans = store.Counts()
	if orphans != 0 {
		t.Fatalf("容器回归应清除孤儿标记，实际孤儿 %d", orphans)
	}
	if report.Diffs[0].Kind != "updated" && report.Diffs[0].Kind != "unchanged" {
		t.Fatalf("孤儿回归应为 updated/unchanged，实际 %s", report.Diffs[0].Kind)
	}
}

func TestSyncNameConflictNotOverwritten(t *testing.T) {
	store := NewStore(openTestDB(t))
	report := store.Sync([]Snapshot{
		{Name: "dup", ContainerID: "a", Image: "img", Status: "running"},
		{Name: "dup", ContainerID: "b", Image: "img", Status: "running"},
	})
	if len(report.Conflicts) != 1 {
		t.Fatalf("同名容器出现两次应报告冲突，实际 %+v", report.Conflicts)
	}
	if report.Conflicts[0].Name != "dup" {
		t.Fatalf("冲突应为 dup，实际 %+v", report.Conflicts[0])
	}
	// 首个快照仍应导入。
	if len(report.Imported) != 1 {
		t.Fatalf("首个快照应导入，实际 %+v", report.Imported)
	}
}

func TestSettingsKV(t *testing.T) {
	store := NewStore(openTestDB(t))
	if err := store.SetSetting("k", "v1"); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	if value, ok := store.GetSetting("k"); !ok || value != "v1" {
		t.Fatalf("读取应为 v1，实际 %q ok=%v", value, ok)
	}
	if err := store.SetSetting("k", "v2"); err != nil {
		t.Fatalf("更新失败：%v", err)
	}
	if value, ok := store.GetSetting("k"); !ok || value != "v2" {
		t.Fatalf("更新后应为 v2，实际 %q", value)
	}
	if _, ok := store.GetSetting("missing"); ok {
		t.Fatal("不存在的键应返回 ok=false")
	}
}

func TestRecordCreateDeleteCascade(t *testing.T) {
	store := NewStore(openTestDB(t))
	snapshot := Snapshot{Name: "beta", ContainerID: "cb", Image: "img:1", Status: "running"}
	store.RecordCreate(snapshot, `{"name":"beta"}`)
	total, _ := store.Counts()
	if total != 1 {
		t.Fatalf("RecordCreate 后应有 1 实例，实际 %d", total)
	}
	var configCount int
	_ = store.db.QueryRow("SELECT COUNT(*) FROM instance_configs").Scan(&configCount)
	if configCount != 1 {
		t.Fatalf("期望配置应随创建写入，实际 %d 条", configCount)
	}
	store.RecordDelete("beta")
	total, _ = store.Counts()
	if total != 0 {
		t.Fatalf("删除后应无实例，实际 %d", total)
	}
	_ = store.db.QueryRow("SELECT COUNT(*) FROM instance_configs").Scan(&configCount)
	if configCount != 0 {
		t.Fatalf("期望配置应级联删除，实际 %d 条", configCount)
	}
}

func TestBackupAndIntegrity(t *testing.T) {
	store := NewStore(openTestDB(t))
	store.Sync([]Snapshot{{Name: "gamma", ContainerID: "cg", Image: "img", Status: "running"}})
	backupDir := filepath.Join(t.TempDir(), "backups")
	backup, err := store.Backup(backupDir)
	if err != nil {
		t.Fatalf("备份失败：%v", err)
	}
	if backup.Size == 0 || backup.Name == "" {
		t.Fatalf("备份产物异常：%+v", backup)
	}
	if ok, message := store.IntegrityCheck(); !ok {
		t.Fatalf("完整性检查失败：%s", message)
	}
	// 备份文件可独立打开。
	backupDB, err := Open(backup.Path)
	if err != nil {
		t.Fatalf("备份文件无法打开：%v", err)
	}
	defer backupDB.Close()
	var total int
	if err := backupDB.QueryRow("SELECT COUNT(*) FROM instances").Scan(&total); err != nil || total != 1 {
		t.Fatalf("备份应包含 1 条实例记录，实际 %d（err=%v）", total, err)
	}
}

// TestBundleSchemaAndActiveUniqueness 验证 Version 5 四张表与活动计划唯一约束。
func TestBundleSchemaAndActiveUniqueness(t *testing.T) {
	db := openTestDB(t)
	if _, err := Migrate(db, Migrations); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	for _, table := range []string{"bundle_installs", "bundle_install_items", "bundle_license_confirmations", "bundle_acceptances"} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("缺少表 %s：%v", table, err)
		}
	}
	insert := func(id, status string) error {
		_, err := db.Exec(`INSERT INTO bundle_installs (id, bundle_id, bundle_version, variant, target_repository, status, created_at, updated_at)
			VALUES (?, 'h3-image-base', '1.0.0', 'default', 'models', ?, '2026-11-12T00:00:00Z', '2026-11-12T00:00:00Z')`, id, status)
		return err
	}
	if err := insert("install-1", "installing"); err != nil {
		t.Fatalf("首个活动计划应可写入：%v", err)
	}
	if err := insert("install-2", "installing"); err == nil {
		t.Fatal("同一套装+版本+变体+仓库不允许第二个活动计划")
	}
	// 终态计划不受活动唯一约束限制。
	if err := insert("install-3", "files_ready"); err != nil {
		t.Fatalf("终态计划应可共存：%v", err)
	}
	// 子项唯一键。
	if _, err := db.Exec(`INSERT INTO bundle_install_items (install_id, item_key, target_path) VALUES ('install-1','k1','vae/a.safetensors')`); err != nil {
		t.Fatalf("子项写入失败：%v", err)
	}
	if _, err := db.Exec(`INSERT INTO bundle_install_items (install_id, item_key, target_path) VALUES ('install-1','k1','vae/b.safetensors')`); err == nil {
		t.Fatal("(install_id, item_key) 必须唯一")
	}
	// 重复迁移幂等。
	if applied, err := Migrate(db, Migrations); err != nil || applied != 0 {
		t.Fatalf("重复迁移应幂等：applied=%d err=%v", applied, err)
	}
}
