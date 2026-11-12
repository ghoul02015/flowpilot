package bundle

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 持久化错误。
var (
	ErrInstallNotFound = errors.New("安装计划不存在")
	ErrActiveInstall   = errors.New("该套装已有进行中的安装计划")
	ErrItemNotFound    = errors.New("安装项不存在")
)

// Install 是一个安装计划。
type Install struct {
	ID               string        `json:"id"`
	BundleID         string        `json:"bundleId"`
	BundleVersion    string        `json:"bundleVersion"`
	ManifestChecksum string        `json:"manifestChecksum"`
	Variant          string        `json:"variant"`
	SelectedOptions  []string      `json:"selectedOptions"`
	TargetRepository string        `json:"targetRepository"`
	Status           string        `json:"status"`
	CurrentItemKey   string        `json:"currentItemKey"`
	TotalBytes       int64         `json:"totalBytes"`
	CompletedBytes   int64         `json:"completedBytes"`
	ErrorCode        string        `json:"errorCode,omitempty"`
	ErrorMessage     string        `json:"errorMessage,omitempty"`
	RequestKey       string        `json:"-"`
	Operator         string        `json:"operator"`
	RecoveryReason   string        `json:"recoveryReason,omitempty"`
	CreatedAt        time.Time     `json:"createdAt"`
	UpdatedAt        time.Time     `json:"updatedAt"`
	StartedAt        *time.Time    `json:"startedAt,omitempty"`
	FinishedAt       *time.Time    `json:"finishedAt,omitempty"`
	Items            []InstallItem `json:"items,omitempty"`
	// Plan 是安装时的不可变快照，只用于内部执行，不重复对外输出。
	Plan Plan `json:"-"`
}

// InstallItem 是计划中的单个文件项。
type InstallItem struct {
	InstallID      string    `json:"-"`
	ItemKey        string    `json:"itemKey"`
	Ordinal        int       `json:"ordinal"`
	Required       bool      `json:"required"`
	SourceRevision string    `json:"sourceRevision"`
	TargetPath     string    `json:"targetPath"`
	ExpectedBytes  int64     `json:"expectedBytes"`
	ExpectedSHA256 string    `json:"expectedSha256"`
	Status         string    `json:"status"`
	ChildTaskID    string    `json:"childTaskId,omitempty"`
	ActualBytes    int64     `json:"actualBytes"`
	ActualSHA256   string    `json:"actualSha256,omitempty"`
	Attempts       int       `json:"attempts"`
	ErrorCode      string    `json:"errorCode,omitempty"`
	ErrorMessage   string    `json:"errorMessage,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// LicenseConfirmation 是许可确认记录。
type LicenseConfirmation struct {
	ID                     string    `json:"id"`
	BundleID               string    `json:"bundleId"`
	BundleVersion          string    `json:"bundleVersion"`
	AcknowledgementVersion string    `json:"acknowledgementVersion"`
	LicenseSnapshotDate    string    `json:"licenseSnapshotDate"`
	DeliveryMethod         string    `json:"deliveryMethod"`
	Operator               string    `json:"operator"`
	ConfirmedAt            time.Time `json:"confirmedAt"`
}

// Acceptance 是人工验收记录；它与"慧浪认证"是两件事。
type Acceptance struct {
	ID            string          `json:"id"`
	BundleID      string          `json:"bundleId"`
	BundleVersion string          `json:"bundleVersion"`
	InstallID     string          `json:"installId"`
	InstanceName  string          `json:"instanceName"`
	Result        string          `json:"result"`
	Preset        json.RawMessage `json:"preset"`
	Environment   json.RawMessage `json:"environment"`
	Metrics       json.RawMessage `json:"metrics"`
	Notes         string          `json:"notes"`
	Operator      string          `json:"operator"`
	CreatedAt     time.Time       `json:"createdAt"`
}

// Store 是套装领域的 SQLite 存储。
type Store struct{ db *sql.DB }

// NewStore 创建存储。
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func nowText() string { return time.Now().UTC().Format(time.RFC3339) }

func statusIsActive(status string) bool {
	return status == StateQueued || status == StateInstalling || status == StatePaused
}

// CreateInstall 在单个事务内写入计划与全部子项；活动计划冲突返回 ErrActiveInstall。
func (s *Store) CreateInstall(ctx context.Context, install Install, plan Plan) error {
	optionsJSON, err := json.Marshal(install.SelectedOptions)
	if err != nil {
		return err
	}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := nowText()
	startedAt := ""
	if install.Status == StateInstalling || install.Status == StateQueued {
		startedAt = now
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bundle_installs
		(id, bundle_id, bundle_version, manifest_checksum, variant, selected_options_json, target_repository,
		 status, current_item_key, total_bytes, completed_bytes, error_code, error_message, request_key,
		 operator, plan_json, recovery_reason, created_at, updated_at, started_at, finished_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		install.ID, install.BundleID, install.BundleVersion, install.ManifestChecksum, install.Variant,
		string(optionsJSON), install.TargetRepository, install.Status, install.CurrentItemKey,
		install.TotalBytes, install.CompletedBytes, install.ErrorCode, install.ErrorMessage, install.RequestKey,
		install.Operator, string(planJSON), install.RecoveryReason, now, now, startedAt, "")
	if err != nil {
		// 活动计划唯一索引冲突。
		if isUniqueViolation(err) {
			return ErrActiveInstall
		}
		return fmt.Errorf("写入安装计划失败：%w", err)
	}
	for index, item := range plan.Items {
		_, err = tx.ExecContext(ctx, `INSERT INTO bundle_install_items
			(install_id, item_key, ordinal, required, source_revision, target_path, expected_bytes,
			 expected_sha256, status, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			install.ID, item.Key, index+1, boolInt(item.Required), item.Revision, item.TargetRelativePath,
			item.SizeBytes, item.SHA256, ItemMissing, now, now)
		if err != nil {
			return fmt.Errorf("写入安装项 %s 失败：%w", item.Key, err)
		}
	}
	return tx.Commit()
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed") || strings.Contains(err.Error(), "constraint failed")
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// GetInstall 读取计划与子项。
func (s *Store) GetInstall(ctx context.Context, id string) (Install, error) {
	var install Install
	var optionsJSON, planJSON, createdAt, updatedAt, startedAt, finishedAt string
	err := s.db.QueryRowContext(ctx, `SELECT id, bundle_id, bundle_version, manifest_checksum, variant,
		selected_options_json, target_repository, status, current_item_key, total_bytes, completed_bytes,
		error_code, error_message, request_key, operator, plan_json, recovery_reason,
		created_at, updated_at, started_at, finished_at
		FROM bundle_installs WHERE id = ?`, id).Scan(
		&install.ID, &install.BundleID, &install.BundleVersion, &install.ManifestChecksum, &install.Variant,
		&optionsJSON, &install.TargetRepository, &install.Status, &install.CurrentItemKey,
		&install.TotalBytes, &install.CompletedBytes, &install.ErrorCode, &install.ErrorMessage,
		&install.RequestKey, &install.Operator, &planJSON, &install.RecoveryReason,
		&createdAt, &updatedAt, &startedAt, &finishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Install{}, ErrInstallNotFound
	}
	if err != nil {
		return Install{}, err
	}
	_ = json.Unmarshal([]byte(optionsJSON), &install.SelectedOptions)
	_ = json.Unmarshal([]byte(planJSON), &install.Plan)
	install.CreatedAt = parseTime(createdAt)
	install.UpdatedAt = parseTime(updatedAt)
	install.StartedAt = parseOptionalTime(startedAt)
	install.FinishedAt = parseOptionalTime(finishedAt)
	items, err := s.ListItems(ctx, id)
	if err != nil {
		return Install{}, err
	}
	install.Items = items
	return install, nil
}

// FindActive 查找同套装同版本同变体同仓库的活动计划。
func (s *Store) FindActive(ctx context.Context, bundleID, version, variant, repo string) (Install, bool, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM bundle_installs
		WHERE bundle_id=? AND bundle_version=? AND variant=? AND target_repository=?
		  AND status IN (?,?,?) LIMIT 1`,
		bundleID, version, variant, repo, StateQueued, StateInstalling, StatePaused).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Install{}, false, nil
	}
	if err != nil {
		return Install{}, false, err
	}
	install, err := s.GetInstall(ctx, id)
	if err != nil {
		return Install{}, false, err
	}
	return install, true, nil
}

// FindByRequestKey 幂等查询。
func (s *Store) FindByRequestKey(ctx context.Context, requestKey string) (Install, bool, error) {
	if requestKey == "" {
		return Install{}, false, nil
	}
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM bundle_installs WHERE request_key=? LIMIT 1`, requestKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Install{}, false, nil
	}
	if err != nil {
		return Install{}, false, err
	}
	install, err := s.GetInstall(ctx, id)
	if err != nil {
		return Install{}, false, err
	}
	return install, true, nil
}

// MarkInterrupted 把服务重启前仍在活动的计划转为 paused 并记录原因。
func (s *Store) MarkInterrupted(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE bundle_installs
		SET status=?, recovery_reason=?, current_item_key='', updated_at=?
		WHERE status IN (?,?)`,
		StatePaused, "服务重启，待继续", nowText(), StateQueued, StateInstalling)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// UpdateInstall 更新计划状态字段。
func (s *Store) UpdateInstall(ctx context.Context, id string, status, currentItemKey string, completedBytes int64, errorCode, errorMessage string) error {
	finished := ""
	if status == StateFilesReady || status == StateFailed || status == StateCancelled {
		finished = nowText()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE bundle_installs
		SET status=?, current_item_key=?, completed_bytes=?, error_code=?, error_message=?, updated_at=?, finished_at=?
		WHERE id=?`,
		status, currentItemKey, completedBytes, errorCode, errorMessage, nowText(), finished, id)
	return err
}

// ListItems 返回计划的子项（按 ordinal 升序）。
func (s *Store) ListItems(ctx context.Context, installID string) ([]InstallItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT install_id, item_key, ordinal, required, source_revision,
		target_path, expected_bytes, expected_sha256, status, child_task_id, actual_bytes, actual_sha256,
		attempts, error_code, error_message, created_at, updated_at
		FROM bundle_install_items WHERE install_id=? ORDER BY ordinal`, installID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []InstallItem{}
	for rows.Next() {
		var item InstallItem
		var required int
		var createdAt, updatedAt string
		if err := rows.Scan(&item.InstallID, &item.ItemKey, &item.Ordinal, &required, &item.SourceRevision,
			&item.TargetPath, &item.ExpectedBytes, &item.ExpectedSHA256, &item.Status, &item.ChildTaskID,
			&item.ActualBytes, &item.ActualSHA256, &item.Attempts, &item.ErrorCode, &item.ErrorMessage,
			&createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.Required = required == 1
		item.CreatedAt = parseTime(createdAt)
		item.UpdatedAt = parseTime(updatedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

// UpdateItem 更新单个子项状态。
func (s *Store) UpdateItem(ctx context.Context, installID, itemKey, status, childTaskID string, actualBytes int64, actualSHA256, errorCode, errorMessage string, bumpAttempts bool) error {
	attempts := ""
	if bumpAttempts {
		attempts = ", attempts = attempts + 1"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE bundle_install_items SET status=?, child_task_id=?,
		actual_bytes=?, actual_sha256=?, error_code=?, error_message=?, updated_at=?`+attempts+`
		WHERE install_id=? AND item_key=?`,
		status, childTaskID, actualBytes, actualSHA256, errorCode, errorMessage, nowText(), installID, itemKey)
	return err
}

// ConfirmLicense 记录一次许可确认。
func (s *Store) ConfirmLicense(ctx context.Context, confirmation LicenseConfirmation) error {
	if confirmation.ConfirmedAt.IsZero() {
		confirmation.ConfirmedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO bundle_license_confirmations
		(id, bundle_id, bundle_version, acknowledgement_version, license_snapshot_date, delivery_method, operator, confirmed_at, request_id)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		confirmation.ID, confirmation.BundleID, confirmation.BundleVersion, confirmation.AcknowledgementVersion,
		confirmation.LicenseSnapshotDate, confirmation.DeliveryMethod, confirmation.Operator,
		confirmation.ConfirmedAt.Format(time.RFC3339), "")
	return err
}

// LicenseConfirmed 查询同一版本与许可版本是否已确认。
func (s *Store) LicenseConfirmed(ctx context.Context, bundleID, bundleVersion, acknowledgementVersion, operator string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM bundle_license_confirmations
		WHERE bundle_id=? AND bundle_version=? AND acknowledgement_version=? AND operator=?`,
		bundleID, bundleVersion, acknowledgementVersion, operator).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// CreateAcceptance 保存人工验收记录。
func (s *Store) CreateAcceptance(ctx context.Context, acceptance Acceptance) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO bundle_acceptances
		(id, bundle_id, bundle_version, install_id, instance_name, result, preset_json, environment_json,
		 metrics_json, notes, operator, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		acceptance.ID, acceptance.BundleID, acceptance.BundleVersion, acceptance.InstallID,
		acceptance.InstanceName, acceptance.Result, string(acceptance.Preset), string(acceptance.Environment),
		string(acceptance.Metrics), acceptance.Notes, acceptance.Operator,
		acceptance.CreatedAt.Format(time.RFC3339))
	return err
}

// ListAcceptances 分页查询验收历史（时间倒序）。
func (s *Store) ListAcceptances(ctx context.Context, bundleID string, limit, offset int) ([]Acceptance, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM bundle_acceptances WHERE bundle_id=?`, bundleID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, bundle_id, bundle_version, install_id, instance_name,
		result, preset_json, environment_json, metrics_json, notes, operator, created_at
		FROM bundle_acceptances WHERE bundle_id=? ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		bundleID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := []Acceptance{}
	for rows.Next() {
		var item Acceptance
		var preset, environment, metrics, createdAt string
		if err := rows.Scan(&item.ID, &item.BundleID, &item.BundleVersion, &item.InstallID,
			&item.InstanceName, &item.Result, &preset, &environment, &metrics, &item.Notes,
			&item.Operator, &createdAt); err != nil {
			return nil, 0, err
		}
		item.Preset = json.RawMessage(preset)
		item.Environment = json.RawMessage(environment)
		item.Metrics = json.RawMessage(metrics)
		item.CreatedAt = parseTime(createdAt)
		list = append(list, item)
	}
	return list, total, rows.Err()
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func parseOptionalTime(value string) *time.Time {
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	return &parsed
}

// FindActiveByBundle 查找某套装任意变体的活动计划（列表与详情使用）。
func (s *Store) FindActiveByBundle(ctx context.Context, bundleID, version, repo string) (Install, bool, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM bundle_installs
		WHERE bundle_id=? AND bundle_version=? AND target_repository=?
		  AND status IN (?,?,?) ORDER BY updated_at DESC LIMIT 1`,
		bundleID, version, repo, StateQueued, StateInstalling, StatePaused).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Install{}, false, nil
	}
	if err != nil {
		return Install{}, false, err
	}
	install, err := s.GetInstall(ctx, id)
	if err != nil {
		return Install{}, false, err
	}
	return install, true, nil
}

// ListInstalls 返回最近的安装计划（含子项摘要），供统一任务中心聚合。
func (s *Store) ListInstalls(ctx context.Context, limit, offset int) ([]Install, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM bundle_installs`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM bundle_installs ORDER BY updated_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	list := make([]Install, 0, len(ids))
	for _, id := range ids {
		install, err := s.GetInstall(ctx, id)
		if err != nil {
			continue
		}
		list = append(list, install)
	}
	return list, total, nil
}

// DeleteInstall 删除终态安装计划记录；活动计划需先取消。
func (s *Store) DeleteInstall(ctx context.Context, id string) error {
	install, err := s.GetInstall(ctx, id)
	if err != nil {
		return err
	}
	if statusIsActive(install.Status) {
		return ErrActiveInstall
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM bundle_install_items WHERE install_id=?`, id); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM bundle_installs WHERE id=?`, id)
	return err
}
