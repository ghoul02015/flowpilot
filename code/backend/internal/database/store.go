package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

// Snapshot 是 Docker 侧实例事实快照，由调用方从 instance.Instance 转换。
type Snapshot struct {
	Name string
	// UUID 容器标签携带的稳定对象 ID（0.0.9.7 起）；空表示旧实例（导入时生成）。
	UUID            string
	ContainerID     string
	ContainerName   string
	Image           string
	Status          string
	GPU             int
	Port            int
	AutoStart       bool
	ModelsTarget    string
	OutputsTarget   string
	WorkflowsTarget string
	ModelLinks      bool
	CreatedAt       time.Time
}

// FieldDiff 描述一个字段的数据库值、Docker 值与采用值。
type FieldDiff struct {
	Field    string `json:"field"`
	Database string `json:"database"`
	Docker   string `json:"docker"`
	Adopted  string `json:"adopted"`
	Reason   string `json:"reason"`
}

// Conflict 是无法静默处理的同步冲突。
type Conflict struct {
	Name    string `json:"name"`
	Reason  string `json:"reason"`
	Details string `json:"details,omitempty"`
}

// InstanceDiff 是一个实例的完整对账结果。
type InstanceDiff struct {
	Name      string      `json:"name"`
	UUID      string      `json:"uuid"`
	Kind      string      `json:"kind"` // imported | updated | rebuilt | unchanged | orphan
	Orphan    bool        `json:"orphan"`
	Diffs     []FieldDiff `json:"diffs,omitempty"`
	AdoptedTo string      `json:"adoptedTo,omitempty"`
}

// SyncReport 是一次同步（导入→比对→报告）的完整结果。
type SyncReport struct {
	CheckedAt time.Time      `json:"checkedAt"`
	Imported  []string       `json:"imported"`
	Updated   []string       `json:"updated"`
	Rebuilt   []string       `json:"rebuilt"`
	Orphans   []string       `json:"orphans"`
	Conflicts []Conflict     `json:"conflicts"`
	Diffs     []InstanceDiff `json:"diffs"`
	Unchanged int            `json:"unchanged"`
	Duration  string         `json:"duration"`
}

// Summary 是 SyncReport 的持久化摘要（settings 表 sync.last）。
type Summary struct {
	CheckedAt time.Time `json:"checkedAt"`
	Instances int       `json:"instances"`
	Orphans   int       `json:"orphans"`
	Conflicts int       `json:"conflicts"`
	Imported  int       `json:"imported"`
	Updated   int       `json:"updated"`
	Rebuilt   int       `json:"rebuilt"`
	Unchanged int       `json:"unchanged"`
	OK        bool      `json:"ok"`
}

// row 是 instances 表的内存视图。
type row struct {
	UUID            string
	Name            string
	ContainerID     string
	ContainerName   string
	Image           string
	GPU             int
	Port            int
	AutoStart       bool
	ModelsTarget    string
	OutputsTarget   string
	WorkflowsTarget string
	ModelLinks      bool
	CreatedAt       string
	Orphan          bool
}

// Store 封装数据底座操作；串行化写路径（SQLite 单写者）。
type Store struct {
	db *sql.DB
	mu sync.Mutex
}

// NewStore 返回基于已迁移数据库的存储。
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// DB 暴露底层连接（仅用于迁移记录查询等只读场景）。
func (s *Store) DB() *sql.DB { return s.db }

// Sync 将 Docker 侧快照与数据库对账：导入新实例、更新变化、
// 沿用 UUID 记录重建、标记孤儿、报告冲突；任何情况不删除记录。
func (s *Store) Sync(snapshots []Snapshot) SyncReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	started := time.Now()
	// 空切片而非 nil：JSON 序列化为 [] 而非 null，前端可直接 .length。
	report := SyncReport{
		CheckedAt: started.UTC(), Diffs: []InstanceDiff{},
		Imported: []string{}, Updated: []string{}, Rebuilt: []string{},
		Orphans: []string{}, Conflicts: []Conflict{},
	}

	rows := s.loadRows()
	byContainer := map[string]*row{}
	byName := map[string]*row{}
	for i := range rows {
		byContainer[rows[i].ContainerID] = &rows[i]
		byName[rows[i].Name] = &rows[i]
	}

	seenContainers := map[string]bool{}
	seenNames := map[string]bool{}
	for _, snapshot := range snapshots {
		if seenNames[snapshot.Name] {
			report.Conflicts = append(report.Conflicts, Conflict{
				Name:   snapshot.Name,
				Reason: "同名容器出现多次，Docker 标签数据异常",
			})
			continue
		}
		seenNames[snapshot.Name] = true
		seenContainers[snapshot.ContainerID] = true

		if existing, ok := byContainer[snapshot.ContainerID]; ok {
			seenNames[existing.Name] = true
			diff := s.compareAndApply(existing, snapshot, false)
			switch {
			case existing.Orphan:
				// 容器回来了：解除孤儿标记。
				s.clearOrphan(existing.UUID)
				report.Updated = append(report.Updated, snapshot.Name)
				report.Diffs = append(report.Diffs, InstanceDiff{Name: snapshot.Name, UUID: existing.UUID, Kind: "updated", Diffs: diff, AdoptedTo: "docker"})
			case len(diff) > 0:
				report.Updated = append(report.Updated, snapshot.Name)
				report.Diffs = append(report.Diffs, InstanceDiff{Name: snapshot.Name, UUID: existing.UUID, Kind: "updated", Diffs: diff, AdoptedTo: "docker"})
			default:
				report.Unchanged++
				report.Diffs = append(report.Diffs, InstanceDiff{Name: snapshot.Name, UUID: existing.UUID, Kind: "unchanged"})
			}
			continue
		}
		if existing, ok := byName[snapshot.Name]; ok {
			// 同名新容器：视为重建，沿用 UUID 与创建时间。
			seenContainers[existing.ContainerID] = true
			diff := s.compareAndApply(existing, snapshot, true)
			report.Rebuilt = append(report.Rebuilt, snapshot.Name)
			diffs := append([]FieldDiff{{
				Field: "containerId", Database: existing.ContainerID, Docker: snapshot.ContainerID,
				Adopted: snapshot.ContainerID, Reason: "容器重建，沿用实例 UUID 与历史身份",
			}}, diff...)
			report.Diffs = append(report.Diffs, InstanceDiff{Name: snapshot.Name, UUID: existing.UUID, Kind: "rebuilt", Diffs: diffs, AdoptedTo: "docker"})
			continue
		}
		created := s.insertRow(snapshot)
		report.Imported = append(report.Imported, snapshot.Name)
		report.Diffs = append(report.Diffs, InstanceDiff{Name: snapshot.Name, UUID: created, Kind: "imported", AdoptedTo: "docker"})
	}

	for i := range rows {
		if !seenContainers[rows[i].ContainerID] && !rows[i].Orphan {
			s.markOrphan(rows[i].UUID)
			report.Orphans = append(report.Orphans, rows[i].Name)
			report.Diffs = append(report.Diffs, InstanceDiff{Name: rows[i].Name, UUID: rows[i].UUID, Kind: "orphan", Orphan: true})
		}
	}
	report.Duration = time.Since(started).Round(time.Millisecond).String()
	_ = s.saveSummary(report, len(rows))
	return report
}

// compareAndApply 将快照字段写入既有行，返回发生变化的字段差异。
// rebuild 为 true 时一并更新 containerId 与 containerName。
func (s *Store) compareAndApply(target *row, snapshot Snapshot, rebuild bool) []FieldDiff {
	diffs := []FieldDiff{}
	adopt := func(field, oldValue, newValue string) {
		if oldValue != newValue {
			diffs = append(diffs, FieldDiff{Field: field, Database: oldValue, Docker: newValue, Adopted: newValue, Reason: "以 Docker 实际状态为准"})
		}
	}
	adoptInt := func(field string, oldValue, newValue int) {
		if oldValue != newValue {
			diffs = append(diffs, FieldDiff{Field: field, Database: fmt.Sprintf("%d", oldValue), Docker: fmt.Sprintf("%d", newValue), Adopted: fmt.Sprintf("%d", newValue), Reason: "以 Docker 实际状态为准"})
		}
	}
	adoptBool := func(field string, oldValue, newValue bool) {
		if oldValue != newValue {
			diffs = append(diffs, FieldDiff{Field: field, Database: fmt.Sprintf("%t", oldValue), Docker: fmt.Sprintf("%t", newValue), Adopted: fmt.Sprintf("%t", newValue), Reason: "以 Docker 实际状态为准"})
		}
	}
	adopt("image", target.Image, snapshot.Image)
	adopt("modelsTarget", target.ModelsTarget, snapshot.ModelsTarget)
	adopt("outputsTarget", target.OutputsTarget, snapshot.OutputsTarget)
	adopt("workflowsTarget", target.WorkflowsTarget, snapshot.WorkflowsTarget)
	adoptInt("gpu", target.GPU, snapshot.GPU)
	adoptInt("port", target.Port, snapshot.Port)
	adoptBool("autoStart", target.AutoStart, snapshot.AutoStart)
	adoptBool("modelLinks", target.ModelLinks, snapshot.ModelLinks)
	if rebuild {
		adopt("containerId", target.ContainerID, snapshot.ContainerID)
		adopt("containerName", target.ContainerName, snapshot.ContainerName)
	}
	if len(diffs) > 0 || rebuild {
		s.updateRow(target.UUID, snapshot, rebuild)
	}
	return diffs
}

func (s *Store) loadRows() []row {
	results, err := s.db.Query(`SELECT uuid, name, container_id, container_name, image, gpu, port,
		auto_start, models_target, outputs_target, workflows_target, model_links, created_at, orphan FROM instances`)
	if err != nil {
		logSQLError("loadRows", err)
		return nil
	}
	defer results.Close()
	rows := []row{}
	for results.Next() {
		var item row
		var autoStart, modelLinks, orphan int
		if err := results.Scan(&item.UUID, &item.Name, &item.ContainerID, &item.ContainerName, &item.Image,
			&item.GPU, &item.Port, &autoStart, &item.ModelsTarget, &item.OutputsTarget, &item.WorkflowsTarget,
			&modelLinks, &item.CreatedAt, &orphan); err != nil {
			logSQLError("loadRows scan", err)
			return rows
		}
		item.AutoStart = autoStart == 1
		item.ModelLinks = modelLinks == 1
		item.Orphan = orphan == 1
		rows = append(rows, item)
	}
	return rows
}

func (s *Store) insertRow(snapshot Snapshot) string {
	id := snapshot.UUID
	if id == "" {
		id = newUUID()
	}
	createdAt := nowRFC3339()
	if !snapshot.CreatedAt.IsZero() {
		createdAt = snapshot.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.Exec(`INSERT INTO instances (uuid, name, container_id, container_name, image, gpu, port,
		auto_start, models_target, outputs_target, workflows_target, model_links, status, created_at, updated_at, orphan)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		id, snapshot.Name, snapshot.ContainerID, snapshot.ContainerName, snapshot.Image, snapshot.GPU, snapshot.Port,
		boolInt(snapshot.AutoStart), snapshot.ModelsTarget, snapshot.OutputsTarget, snapshot.WorkflowsTarget,
		boolInt(snapshot.ModelLinks), snapshot.Status, createdAt, nowRFC3339())
	if err != nil {
		logSQLError("insertRow "+snapshot.Name, err)
		return ""
	}
	return id
}

func (s *Store) updateRow(uuid string, snapshot Snapshot, rebuild bool) {
	if rebuild {
		_, err := s.db.Exec(`UPDATE instances SET container_id = ?, container_name = ?, image = ?, gpu = ?, port = ?,
			auto_start = ?, models_target = ?, outputs_target = ?, workflows_target = ?, model_links = ?, status = ?, updated_at = ?, orphan = 0
			WHERE uuid = ?`,
			snapshot.ContainerID, snapshot.ContainerName, snapshot.Image, snapshot.GPU, snapshot.Port,
			boolInt(snapshot.AutoStart), snapshot.ModelsTarget, snapshot.OutputsTarget, snapshot.WorkflowsTarget,
			boolInt(snapshot.ModelLinks), snapshot.Status, nowRFC3339(), uuid)
		if err != nil {
			logSQLError("updateRow rebuild "+uuid, err)
		}
		return
	}
	_, err := s.db.Exec(`UPDATE instances SET image = ?, gpu = ?, port = ?, auto_start = ?,
		models_target = ?, outputs_target = ?, workflows_target = ?, model_links = ?, status = ?, updated_at = ?, orphan = 0
		WHERE uuid = ?`,
		snapshot.Image, snapshot.GPU, snapshot.Port, boolInt(snapshot.AutoStart),
		snapshot.ModelsTarget, snapshot.OutputsTarget, snapshot.WorkflowsTarget,
		boolInt(snapshot.ModelLinks), snapshot.Status, nowRFC3339(), uuid)
	if err != nil {
		logSQLError("updateRow "+uuid, err)
	}
}

func (s *Store) markOrphan(uuid string) {
	if _, err := s.db.Exec("UPDATE instances SET orphan = 1, updated_at = ? WHERE uuid = ?", nowRFC3339(), uuid); err != nil {
		logSQLError("markOrphan "+uuid, err)
	}
}

func (s *Store) clearOrphan(uuid string) {
	if _, err := s.db.Exec("UPDATE instances SET orphan = 0, updated_at = ? WHERE uuid = ?", nowRFC3339(), uuid); err != nil {
		logSQLError("clearOrphan "+uuid, err)
	}
}

func (s *Store) saveSummary(report SyncReport, total int) error {
	summary := Summary{
		CheckedAt: report.CheckedAt,
		Instances: total,
		Orphans:   len(report.Orphans),
		Conflicts: len(report.Conflicts),
		Imported:  len(report.Imported),
		Updated:   len(report.Updated),
		Rebuilt:   len(report.Rebuilt),
		Unchanged: report.Unchanged,
		OK:        len(report.Conflicts) == 0,
	}
	payload, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	return s.SetSetting("sync.last", string(payload))
}

// LastSummary 返回最近一次同步摘要；从未同步时 ok=false。
func (s *Store) LastSummary() (Summary, bool) {
	value, ok := s.GetSetting("sync.last")
	if !ok {
		return Summary{}, false
	}
	var summary Summary
	if err := json.Unmarshal([]byte(value), &summary); err != nil {
		return Summary{}, false
	}
	return summary, true
}

// Counts 返回当前实例映射统计：总数与孤儿数。
func (s *Store) Counts() (total int, orphans int) {
	_ = s.db.QueryRow("SELECT COUNT(*) FROM instances").Scan(&total)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM instances WHERE orphan = 1").Scan(&orphans)
	return total, orphans
}

// SetSetting 写入键值配置（upsert）。
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, nowRFC3339())
	if err != nil {
		return fmt.Errorf("写入配置 %s 失败：%w", key, err)
	}
	return nil
}

// GetSetting 读取键值配置。
func (s *Store) GetSetting(key string) (string, bool) {
	var value string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		logSQLError("GetSetting "+key, err)
		return "", false
	}
	return value, true
}

// BackupFile 是一次备份的产物信息。
type BackupFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// Backup 使用 VACUUM INTO 生成一致性备份（WAL 模式下安全，不锁写者）。
func (s *Store) Backup(dir string) (BackupFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := mkdirAll(dir); err != nil {
		return BackupFile{}, err
	}
	name := fmt.Sprintf("flowpilot-%s.db", time.Now().UTC().Format("20060102-150405"))
	target := filepath.Join(dir, name)
	if _, err := s.db.Exec("VACUUM INTO ?", target); err != nil {
		return BackupFile{}, fmt.Errorf("数据库备份失败：%w", err)
	}
	size := fileSize(target)
	return BackupFile{Path: target, Name: name, Size: size}, nil
}

// IntegrityCheck 执行 PRAGMA integrity_check，返回 ok 与错误信息。
func (s *Store) IntegrityCheck() (bool, string) {
	var result string
	if err := s.db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		return false, fmt.Sprintf("integrity_check 执行失败：%v", err)
	}
	return result == "ok", result
}

// RecordCreate 在实例创建后即时写库（尽力路径；遗漏由下次 Sync 修复）。
func (s *Store) RecordCreate(snapshot Snapshot, specJSON string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 优先使用容器标签的稳定 UUID（与 workspace/链接目录一致）；
	// name 冲突（重建换 UUID）由 INSERT OR REPLACE 按 name 唯一索引替换旧行。
	id := snapshot.UUID
	if id == "" {
		id = newUUID()
	}
	createdAt := nowRFC3339()
	if !snapshot.CreatedAt.IsZero() {
		createdAt = snapshot.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	tx, err := s.db.Begin()
	if err != nil {
		logSQLError("RecordCreate begin", err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`INSERT OR REPLACE INTO instances (uuid, name, container_id, container_name, image, gpu, port,
		auto_start, models_target, outputs_target, workflows_target, model_links, status, created_at, updated_at, orphan)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		id, snapshot.Name, snapshot.ContainerID, snapshot.ContainerName, snapshot.Image, snapshot.GPU, snapshot.Port,
		boolInt(snapshot.AutoStart), snapshot.ModelsTarget, snapshot.OutputsTarget, snapshot.WorkflowsTarget,
		boolInt(snapshot.ModelLinks), snapshot.Status, createdAt, nowRFC3339()); err != nil {
		logSQLError("RecordCreate insert "+snapshot.Name, err)
		return
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO instance_configs (uuid, spec_json, updated_at) VALUES (?, ?, ?)`,
		id, specJSON, nowRFC3339()); err != nil {
		logSQLError("RecordCreate config "+snapshot.Name, err)
		return
	}
	if err := tx.Commit(); err != nil {
		logSQLError("RecordCreate commit", err)
	}
}

// InstanceEvent 实例健康/生命周期事件。
type InstanceEvent struct {
	ID           int64  `json:"id"`
	InstanceUUID string `json:"instanceUuid"`
	Name         string `json:"name"`
	TS           string `json:"ts"`
	Kind         string `json:"kind"`
	Detail       string `json:"detail"`
	LatencyMS    int    `json:"latencyMs"`
}

// RecordEvent 写入实例事件（失败仅日志，不阻断）。
// 旧实例无 UUID 时以名称为关联键（instance_uuid 列存名称）。
func (s *Store) RecordEvent(instanceUUID, name, kind, detail string, latencyMS int) {
	key := instanceUUID
	if key == "" {
		key = name
	}
	if key == "" {
		return
	}
	_, err := s.db.Exec(`INSERT INTO instance_events (instance_uuid, name, ts, kind, detail, latency_ms)
		VALUES (?, ?, ?, ?, ?, ?)`,
		key, name, nowRFC3339(), kind, detail, latencyMS)
	if err != nil {
		logSQLError("RecordEvent "+kind+" "+name, err)
	}
}

// LoadSpecRaw 返回实例期望配置原始 JSON（instance_configs.spec_json）。
func (s *Store) LoadSpecRaw(name string) (string, bool) {
	var specJSON string
	err := s.db.QueryRow("SELECT spec_json FROM instance_configs WHERE uuid = (SELECT uuid FROM instances WHERE name = ?)", name).Scan(&specJSON)
	if err != nil || specJSON == "" {
		return "", false
	}
	return specJSON, true
}

// ListEvents 按实例倒序查询事件；instanceUUID 为空时查全部。
func (s *Store) ListEvents(instanceUUID string, limit int) []InstanceEvent {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	query := "SELECT id, instance_uuid, name, ts, kind, detail, latency_ms FROM instance_events"
	args := []any{}
	if instanceUUID != "" {
		query += " WHERE instance_uuid = ?"
		args = append(args, instanceUUID)
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		logSQLError("ListEvents", err)
		return []InstanceEvent{}
	}
	defer rows.Close()
	list := []InstanceEvent{}
	for rows.Next() {
		var item InstanceEvent
		if err := rows.Scan(&item.ID, &item.InstanceUUID, &item.Name, &item.TS, &item.Kind, &item.Detail, &item.LatencyMS); err == nil {
			list = append(list, item)
		}
	}
	return list
}

// MarkOrphanByName 将记录标记为孤儿（保留数据删除实例时调用，无需等待下次同步）。
func (s *Store) MarkOrphanByName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec("UPDATE instances SET orphan = 1, updated_at = ? WHERE name = ?", nowRFC3339(), name); err != nil {
		logSQLError("MarkOrphanByName "+name, err)
	}
}

// FindOrphanByName 返回孤儿记录（容器已删、数据保留）的 UUID，
// 供实例重建时沿用 workspace 与对象身份。
func (s *Store) FindOrphanByName(name string) (string, bool) {
	var uuid string
	var orphan int
	err := s.db.QueryRow("SELECT uuid, orphan FROM instances WHERE name = ?", name).Scan(&uuid, &orphan)
	if err != nil || orphan != 1 || uuid == "" {
		return "", false
	}
	return uuid, true
}

// RecordDelete 在实例删除后移除其记录与期望配置（级联）。
func (s *Store) RecordDelete(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec("DELETE FROM instances WHERE name = ?", name); err != nil {
		logSQLError("RecordDelete "+name, err)
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
