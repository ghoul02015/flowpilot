package database

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
)

// Migration 是一次 schema 变更：版本号全局唯一且递增，
// SQL 必须幂等可重放（使用 IF NOT EXISTS 等防御写法），
// checksum 用于防止已应用迁移的内容被静默篡改。
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Migrate 按版本顺序执行迁移：未应用的在事务内执行并记录；
// 已应用的校验 checksum，不匹配立即报错（不静默跳过或覆盖）。
func Migrate(db *sql.DB, migrations []Migration) (int, error) {
	if err := ensureMigrationTable(db); err != nil {
		return 0, err
	}
	applied := 0
	for _, migration := range sortedMigrations(migrations) {
		state, err := migrationState(db, migration.Version)
		if err != nil {
			return applied, err
		}
		checksum := migrationChecksum(migration)
		if state.applied {
			if state.checksum != checksum {
				return applied, fmt.Errorf("迁移 %d(%s) checksum 不匹配：已应用 %s，当前 %s；拒绝继续执行", migration.Version, migration.Name, state.checksum, checksum)
			}
			continue
		}
		if err := applyMigration(db, migration, checksum); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

// SchemaVersion 返回当前已应用的最高迁移版本（空库为 0）。
func SchemaVersion(db *sql.DB) (int, error) {
	if err := ensureMigrationTable(db); err != nil {
		return 0, err
	}
	var version int
	if err := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return 0, fmt.Errorf("读取 schema 版本失败：%w", err)
	}
	return version, nil
}

// AppliedMigration 是已应用迁移的对外视图。
type AppliedMigration struct {
	Version   int    `json:"version"`
	Name      string `json:"name"`
	AppliedAt string `json:"appliedAt"`
	Result    string `json:"result"`
}

// AppliedMigrations 列出全部已应用迁移（版本升序）。
func AppliedMigrations(db *sql.DB) ([]AppliedMigration, error) {
	rows, err := db.Query("SELECT version, name, applied_at, result FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("查询迁移记录失败：%w", err)
	}
	defer rows.Close()
	list := make([]AppliedMigration, 0)
	for rows.Next() {
		var item AppliedMigration
		if err := rows.Scan(&item.Version, &item.Name, &item.AppliedAt, &item.Result); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func ensureMigrationTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		checksum TEXT NOT NULL,
		applied_at TEXT NOT NULL,
		result TEXT NOT NULL DEFAULT 'ok'
	)`)
	if err != nil {
		return fmt.Errorf("创建迁移记录表失败：%w", err)
	}
	return nil
}

type appliedState struct {
	applied  bool
	checksum string
}

func migrationState(db *sql.DB, version int) (appliedState, error) {
	var checksum string
	err := db.QueryRow("SELECT checksum FROM schema_migrations WHERE version = ?", version).Scan(&checksum)
	if err == sql.ErrNoRows {
		return appliedState{}, nil
	}
	if err != nil {
		return appliedState{}, fmt.Errorf("查询迁移状态失败：%w", err)
	}
	return appliedState{applied: true, checksum: checksum}, nil
}

func applyMigration(db *sql.DB, migration Migration, checksum string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("迁移 %d 开启事务失败：%w", migration.Version, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(migration.SQL); err != nil {
		return fmt.Errorf("迁移 %d(%s) 执行失败：%w", migration.Version, migration.Name, err)
	}
	if _, err := tx.Exec(
		"INSERT INTO schema_migrations (version, name, checksum, applied_at, result) VALUES (?, ?, ?, ?, 'ok')",
		migration.Version, migration.Name, checksum, nowRFC3339(),
	); err != nil {
		return fmt.Errorf("迁移 %d 写入记录失败：%w", migration.Version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("迁移 %d 提交失败：%w", migration.Version, err)
	}
	return nil
}

func migrationChecksum(migration Migration) string {
	sum := sha256.Sum256([]byte(migration.SQL))
	return hex.EncodeToString(sum[:])
}

func sortedMigrations(migrations []Migration) []Migration {
	ordered := append([]Migration(nil), migrations...)
	// 稳定插入排序：迁移数量小，无需引入 sort 之外的复杂度。
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j].Version < ordered[j-1].Version; j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	return ordered
}
