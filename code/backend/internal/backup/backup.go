// Package backup 平台备份：SQLite 一致性快照＋实例 workspace（配置/工作流/输出/用户数据）
// 打包为带 manifest 的 tar.gz；模型与镜像仅记录来源信息不重复打包。
// 备份经统一任务协议异步执行（0.0.9.12）。
package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Manifest 备份清单。
type Manifest struct {
	CreatedAt     string          `json:"createdAt"`
	Version       string          `json:"version"`    // 平台版本（调用方传入）
	Components    map[string]bool `json:"components"` // sqlite / workspaces / network-config
	Checksum      string          `json:"checksum"`   // 备份文件 sha256
	SizeBytes     int64           `json:"sizeBytes"`
	InstanceCount int             `json:"instanceCount"`
	Notes         string          `json:"notes"`
}

// Result 备份结果。
type Result struct {
	Path      string   `json:"path"`
	Name      string   `json:"name"`
	SizeBytes int64    `json:"sizeBytes"`
	Checksum  string   `json:"checksum"`
	Manifest  Manifest `json:"manifest"`
}

// Runner 备份执行器。
type Runner struct {
	dbPath         string // SQLite 数据库文件（VACUUM INTO 用 *sql.DB，见下方 Exec）
	backupsRoot    string
	workspacesRoot string // shared/instances
	networkConfig  string // shared/network-storage.json（可空）
}

// NewRunner 创建备份执行器。
func NewRunner(backupsRoot, workspacesRoot, networkConfig string) *Runner {
	return &Runner{backupsRoot: backupsRoot, workspacesRoot: workspacesRoot, networkConfig: networkConfig}
}

// SetDBPath 记录数据库路径（manifest 说明用）。
func (r *Runner) SetDBPath(path string) { r.dbPath = path }

// Exec 执行备份：临时目录写入 → 完成后原子改名 → 计算 checksum。
// sqliteDump 由调用方提供（VACUUM INTO 实现一致性快照）。
func (r *Runner) Exec(version string, sqliteDump func(target string) error) (Result, error) {
	if err := os.MkdirAll(r.backupsRoot, 0o755); err != nil {
		return Result{}, fmt.Errorf("创建备份目录失败：%w", err)
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	name := fmt.Sprintf("flowpilot-backup-%s.tar.gz", stamp)
	finalPath := filepath.Join(r.backupsRoot, name)
	tempPath := finalPath + ".part"

	manifest := Manifest{
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		Version:    version,
		Components: map[string]bool{},
	}
	// 1. SQLite 一致性快照：独立 .db 文件（VACUUM INTO），与 tar 包并列存放。
	sqliteName := ""
	if sqliteDump != nil {
		dumpPath := filepath.Join(r.backupsRoot, fmt.Sprintf("flowpilot-sqlite-%s.db", stamp))
		if err := sqliteDump(dumpPath); err == nil {
			if info, statErr := os.Stat(dumpPath); statErr == nil {
				manifest.Components["sqlite"] = true
				sqliteName = filepath.Base(dumpPath)
				manifest.SizeBytes += info.Size()
			}
		} else {
			return Result{}, fmt.Errorf("SQLite 快照失败：%w", err)
		}
	}
	// 2. tar.gz 打包实例 workspace 与配置（临时文件 → 原子改名）。
	output, err := os.Create(tempPath)
	if err != nil {
		return Result{}, fmt.Errorf("创建备份临时文件失败：%w", err)
	}
	gzipWriter := gzip.NewWriter(output)
	tarWriter := tar.NewWriter(gzipWriter)
	if r.workspacesRoot != "" {
		if entries, readErr := os.ReadDir(r.workspacesRoot); readErr == nil && len(entries) > 0 {
			if tarErr := addDirToTar(tarWriter, r.workspacesRoot, "instances"); tarErr != nil {
				_ = tarWriter.Close()
				_ = gzipWriter.Close()
				_ = output.Close()
				_ = os.Remove(tempPath)
				return Result{}, fmt.Errorf("备份打包失败：%w", tarErr)
			}
			manifest.InstanceCount = len(entries)
			manifest.Components["workspaces"] = true
		}
	}
	if r.networkConfig != "" {
		if _, statErr := os.Stat(r.networkConfig); statErr == nil {
			_ = addFileToTar(tarWriter, r.networkConfig, "network-storage.json")
			manifest.Components["network-config"] = true
		}
	}
	if err := tarWriter.Close(); err != nil {
		_ = gzipWriter.Close()
		_ = output.Close()
		_ = os.Remove(tempPath)
		return Result{}, fmt.Errorf("备份打包失败：%w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		_ = output.Close()
		_ = os.Remove(tempPath)
		return Result{}, fmt.Errorf("备份压缩失败：%w", err)
	}
	if _, seekErr := output.Seek(0, io.SeekStart); seekErr == nil {
		manifest.Checksum, manifest.SizeBytes, _ = checksumFile(output)
	}
	if err := output.Close(); err != nil {
		_ = os.Remove(tempPath)
		return Result{}, fmt.Errorf("备份文件关闭失败：%w", err)
	}
	// 3. 原子改名（完成后才可见）。
	if err := os.Rename(tempPath, finalPath); err != nil {
		_ = os.Remove(tempPath)
		return Result{}, fmt.Errorf("备份落盘失败：%w", err)
	}
	manifest.Notes = sqliteName
	// sidecar manifest（checksum/size 恢复校验用）。
	sidecarRaw, _ := json.MarshalIndent(manifest, "", "  ")
	_ = os.WriteFile(finalPath+".manifest.json", sidecarRaw, 0o644)
	return Result{Path: finalPath, Name: name, SizeBytes: manifest.SizeBytes, Checksum: manifest.Checksum, Manifest: manifest}, nil
}

// ListBackups 列出已有备份（含 sidecar manifest）。
func (r *Runner) ListBackups() []Result {
	entries, err := os.ReadDir(r.backupsRoot)
	if err != nil {
		return []Result{}
	}
	list := []Result{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".gz" || filepath.Ext(entry.Name()) == ".manifest" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		result := Result{Name: entry.Name(), Path: filepath.Join(r.backupsRoot, entry.Name()), SizeBytes: info.Size()}
		// sidecar manifest。
		if raw, err := os.ReadFile(result.Path + ".manifest.json"); err == nil {
			_ = json.Unmarshal(raw, &result.Manifest)
			if result.Manifest.Checksum != "" {
				result.Checksum = result.Manifest.Checksum
			}
		}
		list = append(list, result)
	}
	return list
}

// addFileToTar 写入单文件。
func addFileToTar(writer *tar.Writer, path, name string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: int64(info.Mode().Perm()), Size: info.Size(), ModTime: info.ModTime()}); err != nil {
		return err
	}
	_, err = io.Copy(writer, file)
	return err
}

// addDirToTar 递归写入目录。
func addDirToTar(writer *tar.Writer, root, prefix string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.Join(prefix, relative)
		if entry.IsDir() {
			return writer.WriteHeader(&tar.Header{Name: name + "/", Mode: 0o755, Typeflag: tar.TypeDir, ModTime: time.Now()})
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		return addFileToTar(writer, path, name)
	})
}

// checksumFile 计算文件 sha256 与大小。
func checksumFile(file *os.File) (string, int64, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", 0, err
	}
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}
