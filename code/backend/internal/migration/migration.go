// Package migration 实例历史数据迁移：将旧共享 outputs／workflows
// 归属到指定实例的独立 workspace。执行前自动备份，执行后校验数量与字节。
package migration

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DirectoryStats 目录统计。
type DirectoryStats struct {
	Files        int64  `json:"files"`
	Bytes        int64  `json:"bytes"`
	Exists       bool   `json:"exists"`
	LastModified string `json:"lastModified,omitempty"`
}

// ScanReport 旧共享目录扫描结果。
type ScanReport struct {
	ScannedAt time.Time      `json:"scannedAt"`
	Outputs   DirectoryStats `json:"outputs"`
	Workflows DirectoryStats `json:"workflows"`
}

// PreviewPlan 迁移预览。
type PreviewPlan struct {
	Instance    string                    `json:"instance"`
	Workspace   string                    `json:"workspace"`
	Available   bool                      `json:"available"`
	Reason      string                    `json:"reason,omitempty"`
	Scope       map[string]DirectoryStats `json:"scope"`
	TotalFiles  int64                     `json:"totalFiles"`
	TotalBytes  int64                     `json:"totalBytes"`
	BackupBytes int64                     `json:"backupBytes"`
}

// RunResult 迁移执行结果。
type RunResult struct {
	Instance    string           `json:"instance"`
	Mode        string           `json:"mode"`
	Scope       []string         `json:"scope"`
	BackupDir   string           `json:"backupDir"`
	Copied      map[string]int64 `json:"copiedFiles"`
	CopiedBytes map[string]int64 `json:"copiedBytes"`
	Verified    bool             `json:"verified"`
	Duration    string           `json:"duration"`
	Warnings    []string         `json:"warnings"`
}

// Manager 迁移管理器；sharedRoot 为平台容器内共享数据根（含 outputs/workflows），
// backupsRoot 为备份目录，workspacePath 回调由实例管理器提供。
type Manager struct {
	sharedRoot    string
	backupsRoot   string
	workspacePath func(instance string) (string, bool)
}

// NewManager 创建迁移管理器。
func NewManager(sharedRoot, backupsRoot string, workspacePath func(instance string) (string, bool)) *Manager {
	return &Manager{sharedRoot: sharedRoot, backupsRoot: backupsRoot, workspacePath: workspacePath}
}

// Scan 统计旧共享 outputs／workflows。
func (m *Manager) Scan() ScanReport {
	report := ScanReport{ScannedAt: time.Now().UTC()}
	report.Outputs = statDirectory(filepath.Join(m.sharedRoot, "outputs"))
	report.Workflows = statDirectory(filepath.Join(m.sharedRoot, "workflows"))
	return report
}

// Preview 为指定实例生成迁移计划。
func (m *Manager) Preview(instance string) PreviewPlan {
	plan := PreviewPlan{Instance: instance, Scope: map[string]DirectoryStats{}}
	workspace, ok := m.workspacePath(instance)
	if !ok {
		plan.Reason = "实例不存在独立 workspace（旧实例请先重建，或实例已删除）"
		return plan
	}
	plan.Workspace = workspace
	plan.Available = true
	for _, scope := range []string{"outputs", "workflows"} {
		stats := statDirectory(filepath.Join(m.sharedRoot, scope))
		plan.Scope[scope] = stats
		plan.TotalFiles += stats.Files
		plan.TotalBytes += stats.Bytes
	}
	// 备份空间 = 源数据全量（copy 模式源保留，备份再加一份）。
	plan.BackupBytes = plan.TotalBytes
	return plan
}

// Run 执行迁移：备份 → 复制/移动 → 校验。
func (m *Manager) Run(instance string, scope []string, mode string) (RunResult, error) {
	if mode != "copy" && mode != "move" {
		return RunResult{}, fmt.Errorf("mode 仅支持 copy 或 move")
	}
	workspace, ok := m.workspacePath(instance)
	if !ok {
		return RunResult{}, fmt.Errorf("实例 %s 无独立 workspace（旧实例请先重建）", instance)
	}
	validScope := make([]string, 0, len(scope))
	for _, item := range scope {
		if item == "outputs" || item == "workflows" {
			validScope = append(validScope, item)
		}
	}
	if len(validScope) == 0 {
		return RunResult{}, fmt.Errorf("scope 至少包含 outputs 或 workflows")
	}
	started := time.Now()
	result := RunResult{
		Instance: instance, Mode: mode, Scope: validScope,
		Copied: map[string]int64{}, CopiedBytes: map[string]int64{}, Warnings: []string{},
	}

	// 1. 备份（未成功不进入下一步）。
	backupDir := filepath.Join(m.backupsRoot, "migration-"+time.Now().UTC().Format("20060102-150405"))
	for _, item := range validScope {
		source := filepath.Join(m.sharedRoot, item)
		if _, err := os.Stat(source); os.IsNotExist(err) {
			continue
		}
		target := filepath.Join(backupDir, item)
		if err := copyTree(source, target); err != nil {
			return result, fmt.Errorf("迁移前备份失败（%s），已中止未改动源数据：%w", item, err)
		}
	}
	result.BackupDir = backupDir

	// 2. 复制／移动。
	for _, item := range validScope {
		source := filepath.Join(m.sharedRoot, item)
		if _, err := os.Stat(source); os.IsNotExist(err) {
			result.Copied[item] = 0
			result.CopiedBytes[item] = 0
			continue
		}
		// 目标：workspace/<item>/，源目录内的文件并入（不覆盖已有同名文件）。
		targetRoot := filepath.Join(workspace, item)
		if err := os.MkdirAll(targetRoot, 0o755); err != nil {
			return result, fmt.Errorf("创建目标目录失败：%w", err)
		}
		files, bytes, skipped, err := transferTree(source, targetRoot, mode == "move")
		if err != nil {
			return result, fmt.Errorf("迁移 %s 失败（已复制部分保留，源数据完整，可重试）：%w", item, err)
		}
		result.Copied[item] = files
		result.CopiedBytes[item] = bytes
		if skipped > 0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s：跳过 %d 个目标已存在的同名文件", item, skipped))
		}
	}

	// 3. 校验：目标文件数 ≥ 源文件数（源可能有跳过）；逐 scope 校验字节一致。
	verified := true
	for _, item := range validScope {
		source := filepath.Join(m.sharedRoot, item)
		target := filepath.Join(workspace, item)
		sourceStats := statDirectory(source)
		targetStats := statDirectory(target)
		if mode == "copy" && targetStats.Files < sourceStats.Files {
			verified = false
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s：目标文件数 %d 少于源 %d", item, targetStats.Files, sourceStats.Files))
		}
	}
	result.Verified = verified
	result.Duration = time.Since(started).Round(time.Millisecond).String()
	return result, nil
}

// statDirectory 递归统计文件数与字节数。
func statDirectory(root string) DirectoryStats {
	stats := DirectoryStats{}
	info, err := os.Stat(root)
	if err != nil {
		return stats
	}
	stats.Exists = true
	stats.LastModified = info.ModTime().UTC().Format(time.RFC3339)
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if detail, err := entry.Info(); err == nil {
			stats.Files++
			stats.Bytes += detail.Size()
		}
		return nil
	})
	return stats
}

// copyTree 递归复制目录。
func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		return copyFile(path, destination)
	})
}

// transferTree 复制或移动源目录内容到目标（同名跳过，不覆盖）。
func transferTree(source, target string, removeSource bool) (files, bytes, skipped int64, err error) {
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, relErr := filepath.Rel(source, path)
		if relErr != nil {
			return relErr
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if _, statErr := os.Stat(destination); statErr == nil {
			skipped++
			return nil
		}
		if copyErr := copyFile(path, destination); copyErr != nil {
			return copyErr
		}
		if detail, infoErr := entry.Info(); infoErr == nil {
			files++
			bytes += detail.Size()
		}
		if removeSource {
			if removeErr := os.Remove(path); removeErr != nil {
				return removeErr
			}
		}
		return nil
	})
	// move 模式清理源目录中的空目录。
	if removeSource && err == nil {
		_ = os.Remove(source)
	}
	return files, bytes, skipped, err
}

// copyFile 流式复制单文件（0600 保持权限位）。
func copyFile(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	entry, err := os.Open(source)
	if err != nil {
		return err
	}
	defer entry.Close()
	info, err := entry.Stat()
	if err != nil {
		return err
	}
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer output.Close()
	if _, err := io.Copy(output, entry); err != nil {
		return err
	}
	return nil
}

// SanitizeScope 过滤合法 scope。
func SanitizeScope(scope []string) []string {
	valid := make([]string, 0, len(scope))
	for _, item := range scope {
		if item == "outputs" || item == "workflows" {
			valid = append(valid, strings.TrimSpace(item))
		}
	}
	return valid
}
