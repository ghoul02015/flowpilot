package database

// Migrations 是按版本升序的全部 schema 迁移；只允许追加，不允许修改
// 已发布迁移的 SQL（checksum 校验会拒绝）。
var Migrations = []Migration{
	{
		Version: 1,
		Name:    "core-tables",
		SQL: `CREATE TABLE IF NOT EXISTS instances (
			uuid TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			container_id TEXT NOT NULL DEFAULT '',
			container_name TEXT NOT NULL DEFAULT '',
			image TEXT NOT NULL DEFAULT '',
			gpu INTEGER NOT NULL DEFAULT -1,
			port INTEGER NOT NULL DEFAULT 0,
			container_port INTEGER NOT NULL DEFAULT 0,
			auto_start INTEGER NOT NULL DEFAULT 0,
			models_target TEXT NOT NULL DEFAULT '',
			outputs_target TEXT NOT NULL DEFAULT '',
			workflows_target TEXT NOT NULL DEFAULT '',
			model_links INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			orphan INTEGER NOT NULL DEFAULT 0
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_instances_name ON instances(name);
		CREATE INDEX IF NOT EXISTS idx_instances_container ON instances(container_id);
		CREATE TABLE IF NOT EXISTS instance_configs (
			uuid TEXT PRIMARY KEY REFERENCES instances(uuid) ON DELETE CASCADE,
			spec_json TEXT NOT NULL DEFAULT '{}',
			updated_at TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		);`,
	},
}
