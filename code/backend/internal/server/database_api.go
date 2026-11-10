package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/ghoul02015/flowpilot/internal/database"
	"github.com/ghoul02015/flowpilot/internal/instance"
)

// snapshotOf 将实例视图转换为数据底座同步快照。
func snapshotOf(item instance.Instance) database.Snapshot {
	return database.Snapshot{
		Name:            item.Name,
		ContainerID:     item.ContainerID,
		ContainerName:   item.ContainerName,
		Image:           item.Image,
		Status:          item.Status,
		GPU:             item.GPU,
		Port:            item.Port,
		AutoStart:       item.AutoStart,
		ModelsTarget:    item.ModelsTarget,
		OutputsTarget:   item.OutputsTarget,
		WorkflowsTarget: item.WorkflowsTarget,
		ModelLinks:      item.ModelLinks,
		CreatedAt:       item.CreatedAt,
	}
}

// databaseStatus 返回数据底座状态：schema 版本、迁移记录、映射统计与最近同步摘要。
func (h *Handler) databaseStatus(w http.ResponseWriter, _ *http.Request) {
	if h.database == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "数据底座未启用"})
		return
	}
	migrations, err := database.AppliedMigrations(h.database.DB())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	total, orphans := h.database.Counts()
	summary, _ := h.database.LastSummary()
	integrityOK, integrityMessage := h.database.IntegrityCheck()
	writeJSON(w, http.StatusOK, map[string]any{
		"schemaVersion":    len(migrations),
		"migrations":       migrations,
		"instanceMappings": total,
		"orphanMappings":   orphans,
		"lastSync":         summary,
		"integrity":        map[string]any{"ok": integrityOK, "message": integrityMessage},
		"driver":           "sqlite (modernc.org/sqlite)",
		"journalMode":      "WAL",
	})
}

// databaseCheck 立即执行一次「导入→比对→报告」同步，返回完整对账结果。
func (h *Handler) databaseCheck(w http.ResponseWriter, r *http.Request) {
	if h.database == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "数据底座未启用"})
		return
	}
	var snapshots []database.Snapshot
	if h.instances != nil {
		instances, err := h.instances.List(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "读取实例列表失败：" + err.Error()})
			return
		}
		snapshots = make([]database.Snapshot, 0, len(instances))
		for _, item := range instances {
			snapshots = append(snapshots, snapshotOf(item))
		}
	}
	report := h.database.Sync(snapshots)
	slog.Info("数据底座一致性检查完成",
		"imported", len(report.Imported), "updated", len(report.Updated),
		"rebuilt", len(report.Rebuilt), "orphans", len(report.Orphans),
		"conflicts", len(report.Conflicts), "unchanged", report.Unchanged)
	writeJSON(w, http.StatusOK, report)
}

// databaseBackup 生成 SQLite 一致性备份（VACUUM INTO）。
func (h *Handler) databaseBackup(w http.ResponseWriter, _ *http.Request) {
	if h.database == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "数据底座未启用"})
		return
	}
	backup, err := h.database.Backup(h.backupDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, backup)
}

// recordCreate 是实例创建钩子：写库并保存期望配置快照。
func (h *Handler) recordCreate(spec instance.Spec, created instance.Instance) {
	if h.database == nil {
		return
	}
	specJSON, err := json.Marshal(spec)
	if err != nil {
		specJSON = []byte("{}")
	}
	h.database.RecordCreate(snapshotOf(created), string(specJSON))
}

// recordDelete 是实例删除钩子。
func (h *Handler) recordDelete(name string) {
	if h.database == nil {
		return
	}
	h.database.RecordDelete(name)
}

// syncAtStartup 启动时执行一次同步（导入 0.0.9.4 及之前版本的存量实例）。
func (h *Handler) syncAtStartup(ctx context.Context) {
	if h.database == nil || h.instances == nil {
		return
	}
	instances, err := h.instances.List(ctx)
	if err != nil {
		slog.Error("启动同步失败：无法读取实例列表", "error", err)
		return
	}
	snapshots := make([]database.Snapshot, 0, len(instances))
	for _, item := range instances {
		snapshots = append(snapshots, snapshotOf(item))
	}
	report := h.database.Sync(snapshots)
	slog.Info("数据底座启动同步完成",
		"instances", len(instances), "imported", len(report.Imported),
		"updated", len(report.Updated), "rebuilt", len(report.Rebuilt),
		"orphans", len(report.Orphans), "conflicts", len(report.Conflicts))
}
