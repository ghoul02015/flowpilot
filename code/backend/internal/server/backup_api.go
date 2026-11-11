package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/ghoul02015/flowpilot/internal/backup"
	"github.com/ghoul02015/flowpilot/internal/tasks"
)

// listBackups 备份列表。
func (h *Handler) listBackups(w http.ResponseWriter, _ *http.Request) {
	if h.backupRunner == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "备份未启用"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backups": h.backupRunner.ListBackups()})
}

// createBackup 创建备份任务（统一任务协议：202 返回 taskId，后台执行）。
func (h *Handler) createBackup(w http.ResponseWriter, _ *http.Request) {
	if h.backupRunner == nil || h.database == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "备份未启用（数据底座不可用）"})
		return
	}
	taskID := fmt.Sprintf("backup-%d", time.Now().Unix())
	if h.taskStore != nil {
		h.taskStore.Upsert(tasks.View{
			ID: taskID, Type: "backup", Name: "平台备份",
			Status: tasks.StatusRunning, Progress: -1,
			Message:   "备份执行中（SQLite 快照＋实例数据打包＋checksum）",
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		})
	}
	go func() {
		result, err := h.backupRunner.Exec(Version, func(target string) error {
			_, execErr := h.database.Backup(target)
			return execErr
		})
		if h.taskStore == nil {
			return
		}
		if err != nil {
			h.taskStore.Upsert(tasks.View{ID: taskID, Type: "backup", Name: "平台备份", Status: tasks.StatusFailed, Message: "备份失败", Error: err.Error(), CreatedAt: time.Now().UTC().Format(time.RFC3339)})
			return
		}
		h.taskStore.Upsert(tasks.View{
			ID: taskID, Type: "backup", Name: "平台备份",
			Status: tasks.StatusSucceeded, Progress: 100,
			Message:   fmt.Sprintf("完成：%s（实例 %d 个，含 SQLite/工作区/配置）", result.Name, result.Manifest.InstanceCount),
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		})
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"taskId": taskID, "status": tasks.StatusRunning})
}

var _ = backup.Result{}
