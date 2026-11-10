// Package database 提供嵌入式 SQLite 数据底座：迁移框架、实例映射
// 与键值配置存储。数据库是平台事实的持久层，Docker 仍是运行时状态的
// 唯一来源；两者通过启动同步与手动一致性检查对账。
package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Open 打开（必要时创建）SQLite 数据库并应用连接级设置：
// WAL 提升读写并发，busy_timeout 避免锁冲突立即失败，
// foreign_keys 启用引用完整性。
func Open(path string) (*sql.DB, error) {
	dsn := "file:" + url.PathEscape(path) + "?" + strings.Join([]string{
		"_pragma=journal_mode(WAL)",
		"_pragma=busy_timeout(5000)",
		"_pragma=foreign_keys(ON)",
		"_pragma=synchronous(NORMAL)",
	}, "&")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败：%w", err)
	}
	// modernc 驱动单写多读；限制连接数避免 database is locked。
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("数据库连接失败：%w", err)
	}
	return db, nil
}

// nowRFC3339 返回统一的 UTC 时间戳格式。
func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// logSQLError 统一 SQL 错误日志格式。
func logSQLError(context string, err error) {
	slog.Error("数据库操作失败", "context", context, "error", err)
}
