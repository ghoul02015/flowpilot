// Package storage 管理可选的网络存储（NAS）模型仓库。
//
// 方案：NAS（NFS/SMB）由管理员用平台生成的挂载脚本挂到宿主机固定挂载点
// <hostDataDir>/network（共享数据目录的子目录），平台容器与实例容器
// 通过既有 bind 挂载自动可见；实例侧以 extra_model_paths.yaml 方式
// 让 ComfyUI 原生读取网络仓库，无需拷贝模型即可使用。
package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// MarkerFile 是挂载脚本写入网络仓库根目录的标记文件，平台据此判定挂载生效。
const MarkerFile = ".flowpilot-nas"

// Config 是网络存储配置。
type Config struct {
	Enabled    bool   `json:"enabled"`
	Type       string `json:"type"`       // nfs | smb
	Address    string `json:"address"`    // 服务器地址，如 192.168.0.10
	RemotePath string `json:"remotePath"` // 远端共享路径，如 /volume1/models（NFS）或 models（SMB）
}

// View 是网络存储的对外视图。
type View struct {
	Config
	// MountPoint 是宿主机固定挂载点（共享数据目录下的 network 子目录）。
	MountPoint string `json:"mountPoint"`
	// ContainerPath 是平台容器内对应路径。
	ContainerPath string `json:"containerPath"`
	// Detected 表示已启用且检测到挂载标记（平台容器可见网络仓库）。
	Detected bool `json:"detected"`
}

// ValidationError 表示配置不合法。
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

var (
	addressPattern    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{0,62}$`)
	remotePathPattern = regexp.MustCompile(`^[a-zA-Z0-9_./-]{1,200}$`)
)

// Manager 持久化网络存储配置并探测挂载状态。
type Manager struct {
	storePath     string
	containerPath string // <dataDir>/network
	hostPath      string // <hostDataDir>/network
	yamlDir       string // <dataDir>/network-extra（实例 extra_model_paths.yaml 存放目录，容器内视角）

	mu     sync.Mutex
	config Config
}

// NewManager 创建网络存储管理器并加载既有配置。
func NewManager(storePath, containerPath, hostPath, yamlDir string) *Manager {
	manager := &Manager{storePath: storePath, containerPath: containerPath, hostPath: hostPath, yamlDir: yamlDir}
	data, err := os.ReadFile(storePath)
	if err == nil {
		_ = json.Unmarshal(data, &manager.config)
	}
	return manager
}

// View 返回当前配置与挂载探测结果。
func (m *Manager) View() View {
	m.mu.Lock()
	config := m.config
	m.mu.Unlock()
	return View{
		Config:        config,
		MountPoint:    m.hostPath,
		ContainerPath: m.containerPath,
		Detected:      m.Detected(),
	}
}

// Save 校验并保存配置。
func (m *Manager) Save(config Config) error {
	if config.Enabled {
		if config.Type != "nfs" && config.Type != "smb" {
			return &ValidationError{Message: "存储类型仅支持 nfs 或 smb"}
		}
		if !addressPattern.MatchString(config.Address) {
			return &ValidationError{Message: "服务器地址仅支持主机名或 IP，不含端口与路径"}
		}
		if !remotePathPattern.MatchString(config.RemotePath) || strings.Contains(config.RemotePath, "..") {
			return &ValidationError{Message: "远端共享路径不合法，如 /volume1/models 或 models"}
		}
	}
	m.mu.Lock()
	m.config = config
	m.mu.Unlock()
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.storePath), 0o755); err != nil {
		return err
	}
	tmp := m.storePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.storePath)
}

// Detected 判断网络仓库是否可用：已启用且平台容器能看到挂载标记。
func (m *Manager) Detected() bool {
	m.mu.Lock()
	enabled := m.config.Enabled
	m.mu.Unlock()
	if !enabled {
		return false
	}
	info, err := os.Stat(filepath.Join(m.containerPath, MarkerFile))
	return err == nil && info.Mode().IsRegular()
}

// NetworkContainerPath 返回平台容器内的网络仓库路径。
func (m *Manager) NetworkContainerPath() string { return m.containerPath }

// NetworkHostPath 返回宿主机网络仓库路径（实例 bind 挂载源）。
func (m *Manager) NetworkHostPath() string { return m.hostPath }

// WriteExtraModelPaths 为实例生成 extra_model_paths.yaml，
// basePath 是 ComfyUI 在实例容器内的根目录（如 /app/ComfyUI），
// 返回宿主机侧文件路径供 bind 挂载。
func (m *Manager) WriteExtraModelPaths(name, basePath string) (string, error) {
	if err := os.MkdirAll(m.yamlDir, 0o755); err != nil {
		return "", err
	}
	containerFile := filepath.Join(m.yamlDir, name+".yaml")
	content := renderExtraModelPaths(basePath)
	tmp := containerFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, containerFile); err != nil {
		return "", err
	}
	// yamlDir 位于共享数据目录下，宿主机路径 = 宿主机根 + 相对路径。
	relative, err := filepath.Rel(filepath.Dir(m.containerPath), containerFile)
	if err != nil {
		return "", fmt.Errorf("推导宿主机路径失败：%w", err)
	}
	return filepath.Join(filepath.Dir(m.hostPath), relative), nil
}

// RemoveExtraModelPaths 删除实例的 extra_model_paths.yaml（实例删除时清理）。
func (m *Manager) RemoveExtraModelPaths(name string) {
	_ = os.Remove(filepath.Join(m.yamlDir, name+".yaml"))
}

// extraModelCategories 与 ComfyUI folder_paths 支持的目录约定对齐；
// 不含 custom_nodes（网络仓库不应注入可执行代码）。
var extraModelCategories = []string{
	"checkpoints", "loras", "vae", "controlnet", "embeddings", "upscale_models",
	"text_encoders", "diffusion_models", "clip_vision", "model_patches",
	"clip", "unet", "vae_approx", "style_models", "gligen", "hypernetworks",
	"photomaker", "classifiers", "audio_encoders",
}

func renderExtraModelPaths(basePath string) string {
	var builder strings.Builder
	builder.WriteString("# 由 FlowPilot 生成：实例容器内 ComfyUI 自动加载本文件，\n# 将网络存储仓库（NAS）追加为只读模型来源。\nflowpilot_network:\n")
	builder.WriteString("    base_path: " + basePath + "/models-net\n")
	for _, category := range extraModelCategories {
		builder.WriteString("    " + category + ": " + category + "\n")
	}
	return builder.String()
}

// MountScript 生成宿主机一键挂载脚本（管理员在目标机器执行一次）。
func (m *Manager) MountScript() (string, error) {
	m.mu.Lock()
	config := m.config
	m.mu.Unlock()
	if !config.Enabled {
		return "", &ValidationError{Message: "请先启用并保存网络存储配置"}
	}
	return renderMountScript(config, m.hostPath), nil
}
