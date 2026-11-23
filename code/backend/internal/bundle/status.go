package bundle

import (
	"strings"
	"time"
)

// RepoSnapshot 是一次目标仓库扫描结果，五个套装共享，避免逐卡片重复扫盘。
type RepoSnapshot struct {
	Root      string           `json:"root"`
	Sizes     map[string]int64 `json:"-"` // 相对路径 -> 字节数
	ScannedAt time.Time        `json:"scannedAt"`
	Available bool             `json:"available"`
}

// NewRepoSnapshot 由相对路径与大小构造快照。
func NewRepoSnapshot(root string, sizes map[string]int64) RepoSnapshot {
	if sizes == nil {
		sizes = map[string]int64{}
	}
	return RepoSnapshot{Root: root, Sizes: sizes, ScannedAt: time.Now().UTC(), Available: true}
}

// ItemState 是单个文件项的判定结果。
type ItemState struct {
	Key           string `json:"key"`
	Role          string `json:"role"`
	Required      bool   `json:"required"`
	TargetPath    string `json:"targetPath"`
	Status        string `json:"status"`
	ExpectedBytes int64  `json:"expectedBytes"`
	ActualBytes   int64  `json:"actualBytes"`
	Reason        string `json:"reason,omitempty"`
}

// BundleState 是套装的聚合状态。
type BundleState struct {
	BundleID        string      `json:"bundleId"`
	State           string      `json:"state"`
	Items           []ItemState `json:"items"`
	RequiredTotal   int         `json:"requiredTotal"`
	RequiredPresent int         `json:"requiredPresent"`
	OptionalTotal   int         `json:"optionalTotal"`
	OptionalPresent int         `json:"optionalPresent"`
	RequiredBytes   int64       `json:"requiredBytes"`
	PresentBytes    int64       `json:"presentBytes"`
	MissingBytes    int64       `json:"missingBytes"`
	ScannedAt       time.Time   `json:"scannedAt"`
}

// ComputeState 计算套装与逐项状态。
// verifiedHashes 只包含"已完整校验且路径与大小未变化"的文件，用于免重复 hash。
func ComputeState(b *Bundle, snapshot RepoSnapshot, verifiedHashes map[string]string) BundleState {
	state := BundleState{BundleID: b.ID, Items: []ItemState{}, ScannedAt: snapshot.ScannedAt}
	for _, item := range b.Items {
		expected := item.Sources[0]
		actualSize, exists := snapshot.Sizes[item.TargetRelativePath]
		itemState := ItemState{
			Key:           item.Key,
			Role:          item.Role,
			Required:      item.Required,
			TargetPath:    item.TargetRelativePath,
			ExpectedBytes: expected.SizeBytes,
		}
		switch {
		case !exists:
			itemState.Status = ItemMissing
		case snapshot.Available && actualSize != expected.SizeBytes:
			itemState.Status = ItemHashMismatch
			itemState.ActualBytes = actualSize
			itemState.Reason = "文件大小与清单不一致"
		default:
			itemState.Status = ItemPresent
			itemState.ActualBytes = actualSize
			if hash, ok := verifiedHashes[item.TargetRelativePath]; ok && !strings.EqualFold(hash, expected.SHA256) {
				itemState.Status = ItemConflict
				itemState.Reason = "已校验内容与清单哈希不一致"
			}
		}
		state.Items = append(state.Items, itemState)
		if item.Required {
			state.RequiredTotal++
			state.RequiredBytes += expected.SizeBytes
			if itemState.Status == ItemPresent {
				state.RequiredPresent++
				state.PresentBytes += expected.SizeBytes
			} else {
				state.MissingBytes += expected.SizeBytes
			}
		} else {
			state.OptionalTotal++
			if itemState.Status == ItemPresent {
				state.OptionalPresent++
			}
		}
	}
	switch {
	case state.RequiredPresent == state.RequiredTotal && state.RequiredTotal > 0:
		state.State = StateFilesReady
	case state.RequiredPresent == 0:
		state.State = StateNotInstalled
	default:
		state.State = StatePartial
	}
	return state
}

// OverlayInstallStatus 用安装计划状态覆盖文件状态（活动计划优先）。
func (s *BundleState) OverlayInstallStatus(install *Install) {
	if install == nil {
		return
	}
	switch install.Status {
	case StateQueued, StateInstalling, StatePaused, StateFailed, StateCancelled:
		s.State = install.Status
	}
}
