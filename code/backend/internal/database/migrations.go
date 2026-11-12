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
	{
		Version: 2,
		Name:    "auth-and-audit",
		SQL: `CREATE TABLE IF NOT EXISTS users (
			username TEXT PRIMARY KEY,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			csrf_token TEXT NOT NULL,
			created_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			idle_expires_at TEXT NOT NULL,
			absolute_expires_at TEXT NOT NULL,
			revoked INTEGER NOT NULL DEFAULT 0,
			ip_hash TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_sessions_username ON sessions(username);
		CREATE TABLE IF NOT EXISTS audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts TEXT NOT NULL,
			actor TEXT NOT NULL,
			action TEXT NOT NULL,
			object TEXT NOT NULL DEFAULT '',
			result TEXT NOT NULL,
			ip_hash TEXT NOT NULL DEFAULT '',
			request_id TEXT NOT NULL DEFAULT '',
			detail TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_audit_ts ON audit_logs(ts);`,
	},
	{
		Version: 3,
		Name:    "instance-events",
		SQL: `CREATE TABLE IF NOT EXISTS instance_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			instance_uuid TEXT NOT NULL,
			name TEXT NOT NULL,
			ts TEXT NOT NULL,
			kind TEXT NOT NULL,
			detail TEXT NOT NULL DEFAULT '',
			latency_ms INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_events_uuid_ts ON instance_events(instance_uuid, id);`,
	},
	{
		Version: 4,
		Name:    "unified-tasks",
		SQL: `CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			name TEXT NOT NULL,
			object_type TEXT NOT NULL DEFAULT '',
			object_ref TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			progress INTEGER NOT NULL DEFAULT -1,
			message TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			cancelable INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			started_at TEXT NOT NULL DEFAULT '',
			finished_at TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
		CREATE INDEX IF NOT EXISTS idx_tasks_created ON tasks(created_at);`,
	},
	{
		Version: 5,
		Name:    "model-bundles",
		SQL: `CREATE TABLE IF NOT EXISTS bundle_installs (
			id TEXT PRIMARY KEY,
			bundle_id TEXT NOT NULL,
			bundle_version TEXT NOT NULL,
			manifest_checksum TEXT NOT NULL DEFAULT '',
			variant TEXT NOT NULL DEFAULT '',
			selected_options_json TEXT NOT NULL DEFAULT '[]',
			target_repository TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			current_item_key TEXT NOT NULL DEFAULT '',
			total_bytes INTEGER NOT NULL DEFAULT 0,
			completed_bytes INTEGER NOT NULL DEFAULT 0,
			error_code TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			request_key TEXT NOT NULL DEFAULT '',
			operator TEXT NOT NULL DEFAULT '',
			plan_json TEXT NOT NULL DEFAULT '{}',
			recovery_reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL DEFAULT '',
			finished_at TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_bundle_installs_bundle ON bundle_installs(bundle_id);
		CREATE INDEX IF NOT EXISTS idx_bundle_installs_status ON bundle_installs(status);
		CREATE INDEX IF NOT EXISTS idx_bundle_installs_updated ON bundle_installs(updated_at);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_bundle_installs_request ON bundle_installs(request_key) WHERE request_key <> '';
		CREATE UNIQUE INDEX IF NOT EXISTS idx_bundle_installs_active ON bundle_installs(bundle_id, bundle_version, variant, target_repository)
			WHERE status IN ('queued', 'installing', 'paused');

		CREATE TABLE IF NOT EXISTS bundle_install_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			install_id TEXT NOT NULL REFERENCES bundle_installs(id) ON DELETE CASCADE,
			item_key TEXT NOT NULL,
			ordinal INTEGER NOT NULL DEFAULT 0,
			required INTEGER NOT NULL DEFAULT 0,
			source_revision TEXT NOT NULL DEFAULT '',
			target_path TEXT NOT NULL DEFAULT '',
			expected_bytes INTEGER NOT NULL DEFAULT 0,
			expected_sha256 TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			child_task_id TEXT NOT NULL DEFAULT '',
			actual_bytes INTEGER NOT NULL DEFAULT 0,
			actual_sha256 TEXT NOT NULL DEFAULT '',
			attempts INTEGER NOT NULL DEFAULT 0,
			error_code TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			UNIQUE (install_id, item_key)
		);
		CREATE INDEX IF NOT EXISTS idx_bundle_items_install ON bundle_install_items(install_id);
		CREATE INDEX IF NOT EXISTS idx_bundle_items_status ON bundle_install_items(status);
		CREATE INDEX IF NOT EXISTS idx_bundle_items_child ON bundle_install_items(child_task_id);

		CREATE TABLE IF NOT EXISTS bundle_license_confirmations (
			id TEXT PRIMARY KEY,
			bundle_id TEXT NOT NULL,
			bundle_version TEXT NOT NULL,
			acknowledgement_version TEXT NOT NULL,
			license_snapshot_date TEXT NOT NULL DEFAULT '',
			delivery_method TEXT NOT NULL DEFAULT '',
			operator TEXT NOT NULL DEFAULT '',
			confirmed_at TEXT NOT NULL DEFAULT '',
			request_id TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_bundle_license_bundle ON bundle_license_confirmations(bundle_id, bundle_version, confirmed_at);

		CREATE TABLE IF NOT EXISTS bundle_acceptances (
			id TEXT PRIMARY KEY,
			bundle_id TEXT NOT NULL,
			bundle_version TEXT NOT NULL,
			install_id TEXT NOT NULL DEFAULT '',
			instance_name TEXT NOT NULL DEFAULT '',
			result TEXT NOT NULL DEFAULT '',
			preset_json TEXT NOT NULL DEFAULT '{}',
			environment_json TEXT NOT NULL DEFAULT '{}',
			metrics_json TEXT NOT NULL DEFAULT '{}',
			notes TEXT NOT NULL DEFAULT '',
			operator TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_bundle_acceptances_bundle ON bundle_acceptances(bundle_id, created_at);
		CREATE INDEX IF NOT EXISTS idx_bundle_acceptances_instance ON bundle_acceptances(instance_name);`,
	},
}
