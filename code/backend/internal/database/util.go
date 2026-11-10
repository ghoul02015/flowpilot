package database

import (
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// newUUID 生成稳定对象 ID（UUIDv4）。
func newUUID() string { return uuid.NewString() }

// mkdirAll 建目录（权限 0o755，已是目录时幂等）。
func mkdirAll(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return nil
}

// fileSize 返回文件大小；不存在时为 0。
func fileSize(path string) int64 {
	info, err := os.Stat(filepath.Clean(path))
	if err != nil {
		return 0
	}
	return info.Size()
}
