// Package bundle 实现模型组合套装（bundle）的清单加载、校验、
// 状态计算、预检与安装编排。所有类型与 HTTP 无关，便于单测。
package bundle

// Manifest 是随二进制发布的首版套装清单。
type Manifest struct {
	ManifestVersion string           `json:"manifestVersion"`
	BundleVersion   string           `json:"bundleVersion"`
	GeneratedAt     string           `json:"generatedAt"`
	RepositoryRoot  string           `json:"repositoryRoot"`
	Sources         []ManifestSource `json:"sources"`
	Bundles         []Bundle         `json:"bundles"`

	// Checksum 为原始清单字节的 SHA256，用于把安装、许可和验收绑定到具体内容。
	Checksum string `json:"-"`
}

// ManifestSource 记录一个来源仓库冻结的坐标。
type ManifestSource struct {
	Repo         string `json:"repo"`
	Revision     string `json:"revision"`
	LicenseName  string `json:"licenseName"`
	LastModified string `json:"lastModified"`
}

// Bundle 是一个可预检、可安装的套装。
type Bundle struct {
	ID                      string          `json:"id"`
	Version                 string          `json:"version"`
	Name                    string          `json:"name"`
	ModelName               string          `json:"modelName"`
	Summary                 string          `json:"summary"`
	Capability              []string        `json:"capability"`
	ReleaseChannel          string          `json:"releaseChannel"`
	RequiredComfyUI         string          `json:"requiredComfyUI"`
	RequiredTemplatePack    string          `json:"requiredTemplatePack"`
	Variants                []Variant       `json:"variants"`
	Items                   []Item          `json:"items"`
	HardwareRules           []HardwareRule  `json:"hardwareRules"`
	Workflows               []WorkflowRef   `json:"workflows"`
	License                 License         `json:"license"`
	CertificationByHardware []Certification `json:"certificationByHardware"`
	Experimental            bool            `json:"experimental"`
}

// Variant 是套装的可选组合，例如 Turbo 4-step。
type Variant struct {
	Key           string   `json:"key"`
	Name          string   `json:"name"`
	Default       bool     `json:"default"`
	OptionalGroup string   `json:"optionalGroup,omitempty"`
	Selects       []string `json:"selects,omitempty"`
}

// Item 是套装中的一个文件项。
type Item struct {
	Key                string   `json:"key"`
	Role               string   `json:"role"`
	Required           bool     `json:"required"`
	OptionalGroup      string   `json:"optionalGroup,omitempty"`
	Category           string   `json:"category"`
	FileName           string   `json:"fileName"`
	TargetRelativePath string   `json:"targetRelativePath"`
	DependsOn          []string `json:"dependsOn,omitempty"`
	Conflicts          []string `json:"conflicts,omitempty"`
	Sources            []Source `json:"sources"`
	Note               string   `json:"note,omitempty"`
}

// Source 是一个不可变下载坐标。
type Source struct {
	Repo      string `json:"repo"`
	Revision  string `json:"revision"`
	FilePath  string `json:"filePath"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
}

// HardwareRule 描述某档硬件的匹配结论。
type HardwareRule struct {
	HardwareClass string `json:"hardwareClass"`
	Level         string `json:"level"`
	Note          string `json:"note"`
}

// WorkflowRef 只保存官方引用，不内嵌可执行 Prompt。
type WorkflowRef struct {
	Name             string `json:"name"`
	OfficialURL      string `json:"officialURL"`
	TemplateStatus   string `json:"templateStatus"`
	AcceptancePreset string `json:"acceptancePreset"`
}

// License 是许可快照。
type License struct {
	Name                   string `json:"name"`
	SourceURL              string `json:"sourceURL"`
	SnapshotDate           string `json:"snapshotDate"`
	ReviewStatus           string `json:"reviewStatus"`
	NoticeRequired         bool   `json:"noticeRequired"`
	CommercialUIRequired   bool   `json:"commercialUIRequired"`
	AcknowledgementVersion string `json:"acknowledgementVersion"`
	DeliveryBoundary       string `json:"deliveryBoundary"`
}

// Certification 只能来自后续审核发布，不接受人工验收写入。
type Certification struct {
	HardwareClass string `json:"hardwareClass"`
	Status        string `json:"status"`
	Evidence      string `json:"evidence"`
}

// 文件项状态。
const (
	ItemMissing      = "missing"
	ItemDownloading  = "downloading"
	ItemPaused       = "paused"
	ItemVerifying    = "verifying"
	ItemPresent      = "present"
	ItemHashMismatch = "hash_mismatch"
	ItemConflict     = "conflict"
)

// 套装安装状态。
const (
	StateNotInstalled = "not_installed"
	StatePartial      = "partial"
	StateQueued       = "queued"
	StateInstalling   = "installing"
	StatePaused       = "paused"
	StateFilesReady   = "files_ready"
	StateFailed       = "failed"
	StateCancelled    = "cancelled"
)

// 硬件匹配等级。
const (
	LevelPriority       = "priority"
	LevelCandidate      = "candidate"
	LevelConditional    = "conditional"
	LevelExperimental   = "experimental"
	LevelNotRecommended = "not_recommended"
	LevelNotCommitted   = "not_committed"
	LevelUnknown        = "unknown"
)
