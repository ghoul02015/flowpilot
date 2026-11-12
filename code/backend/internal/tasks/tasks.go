// Package tasks 统一任务模型与持久化：为下载、导入、镜像构建/拉取、
// 数据迁移等任务提供同一套协议（状态、进度、对象关联），
// 供任务中心统一视图查询。
package tasks

import (
	"database/sql"
	"errors"
	"log/slog"
	"time"
)

// 统一状态。
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusPaused    = "paused"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
	StatusPartial   = "partial"
)

// 统一类型。
const (
	TypeDownload      = "download"
	TypeImport        = "import"
	TypeImagePull     = "image-pull"
	TypeImageBuild    = "image-build"
	TypeMigration     = "migration"
	TypeBackup        = "backup"
	TypeBundleInstall = "bundle-install"
)

// 任务记录删除错误。
var (
	ErrNotFound   = errors.New("任务不存在")
	ErrTaskActive = errors.New("任务仍在进行中，请先停止或取消")
)

// View 统一任务视图。
type View struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	ObjectType string `json:"objectType"`
	ObjectRef  string `json:"objectRef"`
	Status     string `json:"status"`
	Progress   int    `json:"progress"` // -1 表示未知总量（不确定进度）
	Message    string `json:"message"`
	Error      string `json:"error,omitempty"`
	Cancelable bool   `json:"cancelable"`
	CreatedAt  string `json:"createdAt"`
	StartedAt  string `json:"startedAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
}

// Store 统一任务存储（SQLite）。
type Store struct {
	db *sql.DB
}

// NewStore 创建任务存储。
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Upsert 写入或更新任务记录。
func (s *Store) Upsert(view View) {
	_, err := s.db.Exec(`INSERT INTO tasks (id, type, name, object_type, object_ref, status, progress, message, error, cancelable, created_at, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status, progress=excluded.progress,
			message=excluded.message, error=excluded.error, finished_at=excluded.finished_at`,
		view.ID, view.Type, view.Name, view.ObjectType, view.ObjectRef, view.Status, view.Progress,
		view.Message, view.Error, boolInt(view.Cancelable), view.CreatedAt, view.StartedAt, view.FinishedAt)
	if err != nil {
		slog.Error("任务记录写入失败", "id", view.ID, "error", err)
	}
}

// Get 按 ID 查询。
func (s *Store) Get(id string) (View, bool) {
	var view View
	var cancelable int
	err := s.db.QueryRow(`SELECT id, type, name, object_type, object_ref, status, progress, message, error, cancelable, created_at, started_at, finished_at
		FROM tasks WHERE id = ?`, id).Scan(&view.ID, &view.Type, &view.Name, &view.ObjectType, &view.ObjectRef, &view.Status, &view.Progress, &view.Message, &view.Error, &cancelable, &view.CreatedAt, &view.StartedAt, &view.FinishedAt)
	if err != nil {
		return View{}, false
	}
	view.Cancelable = cancelable == 1
	return view, true
}

// List 分页筛选查询（时间倒序）；type/status 为空返回全部。
func (s *Store) List(taskType, status string, limit, offset int) ([]View, int) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	where := "1=1"
	args := []any{}
	if taskType != "" {
		where += " AND type = ?"
		args = append(args, taskType)
	}
	if status != "" {
		where += " AND status = ?"
		args = append(args, status)
	}
	var total int
	_ = s.db.QueryRow("SELECT COUNT(*) FROM tasks WHERE "+where, args...).Scan(&total)
	rows, err := s.db.Query("SELECT id, type, name, object_type, object_ref, status, progress, message, error, cancelable, created_at, started_at, finished_at FROM tasks WHERE "+where+" ORDER BY created_at DESC LIMIT ? OFFSET ?",
		append(args, limit, offset)...)
	if err != nil {
		return []View{}, total
	}
	defer rows.Close()
	list := []View{}
	for rows.Next() {
		var view View
		var cancelable int
		if err := rows.Scan(&view.ID, &view.Type, &view.Name, &view.ObjectType, &view.ObjectRef, &view.Status, &view.Progress, &view.Message, &view.Error, &cancelable, &view.CreatedAt, &view.StartedAt, &view.FinishedAt); err == nil {
			view.Cancelable = cancelable == 1
			list = append(list, view)
		}
	}
	return list, total
}

// Delete 删除已结束的持久化任务记录；运行中任务须先停止或取消。
func (s *Store) Delete(id string) error {
	var status string
	err := s.db.QueryRow("SELECT status FROM tasks WHERE id = ?", id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	switch status {
	case StatusQueued, StatusRunning, StatusPaused:
		return ErrTaskActive
	}
	result, err := s.db.Exec("DELETE FROM tasks WHERE id = ?", id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordMigrationTask 迁移任务便捷写入。
func (s *Store) RecordMigrationTask(id, instance string, status string, progress int, message string, taskErr string) {
	now := time.Now().UTC().Format(time.RFC3339)
	s.Upsert(View{
		ID: id, Type: TypeMigration, Name: "数据迁移 → " + instance,
		ObjectType: "instance", ObjectRef: instance,
		Status: status, Progress: progress, Message: message, Error: taskErr,
		CreatedAt: now,
	})
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
