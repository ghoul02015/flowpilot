package tasks

import (
	"path/filepath"
	"testing"

	"github.com/ghoul02015/flowpilot/internal/database"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "tasks-test.db"))
	if err != nil {
		t.Fatalf("打开数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := database.Migrate(db, database.Migrations); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	return NewStore(db)
}

func TestUpsertListFilter(t *testing.T) {
	store := openStore(t)
	store.Upsert(View{ID: "mig-a", Type: TypeMigration, Name: "迁移A", Status: StatusRunning, Progress: -1, CreatedAt: "2026-01-01T00:00:00Z"})
	store.Upsert(View{ID: "dl-b", Type: TypeDownload, Name: "模型B", Status: StatusSucceeded, Progress: 100, CreatedAt: "2026-01-02T00:00:00Z"})
	store.Upsert(View{ID: "mig-c", Type: TypeMigration, Name: "迁移C", Status: StatusFailed, Progress: -1, Error: "空间不足", CreatedAt: "2026-01-03T00:00:00Z"})
	// Upsert 更新状态。
	store.Upsert(View{ID: "mig-a", Type: TypeMigration, Name: "迁移A", Status: StatusSucceeded, Progress: 100, CreatedAt: "2026-01-01T00:00:00Z"})

	all, total := store.List("", "", 10, 0)
	if total != 3 || len(all) != 3 {
		t.Fatalf("应 3 条任务：%d/%d", total, len(all))
	}
	// 时间倒序。
	if all[0].ID != "mig-c" {
		t.Fatalf("应按时间倒序：%+v", all[0])
	}
	byType, _ := store.List(TypeMigration, "", 10, 0)
	if len(byType) != 2 {
		t.Fatalf("类型过滤应 2 条：%d", len(byType))
	}
	byStatus, _ := store.List("", StatusSucceeded, 10, 0)
	if len(byStatus) != 2 {
		t.Fatalf("状态过滤应 2 条：%d", len(byStatus))
	}
	// 分页。
	page, _ := store.List("", "", 2, 0)
	if len(page) != 2 {
		t.Fatalf("分页应 2 条：%d", len(page))
	}
	// 详情。
	view, ok := store.Get("mig-c")
	if !ok || view.Error != "空间不足" {
		t.Fatalf("详情异常：%+v", view)
	}
	if _, ok := store.Get("ghost"); ok {
		t.Fatal("不存在的任务应返回 false")
	}
}
