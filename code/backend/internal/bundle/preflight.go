package bundle

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 预检结论。
const (
	CheckPass    = "pass"
	CheckWarn    = "warn"
	CheckBlock   = "block"
	CheckUnknown = "unknown"
)

// Check 是一条预检结论。
type Check struct {
	Code        string    `json:"code"`
	Status      string    `json:"status"`
	Title       string    `json:"title"`
	Detail      string    `json:"detail"`
	Observed    string    `json:"observed,omitempty"`
	Required    string    `json:"required,omitempty"`
	Remediation string    `json:"remediation,omitempty"`
	CheckedAt   time.Time `json:"checkedAt"`
}

// GPUInfo 是单卡信息，只保留 index 与型号，不计算显存总和。
type GPUInfo struct {
	Index         int    `json:"index"`
	Name          string `json:"name"`
	MemoryTotalMB int64  `json:"memoryTotalMB"`
}

// HardwareSummary 是一次硬件采集摘要。
type HardwareSummary struct {
	GPUs          []GPUInfo `json:"gpus"`
	UnifiedMemory bool      `json:"unifiedMemory"`
	HostModel     string    `json:"hostModel"`
	CollectedAt   time.Time `json:"collectedAt"`
	Error         string    `json:"error,omitempty"`
}

// PreflightResult 是一次预检的完整结果。
type PreflightResult struct {
	BundleID        string    `json:"bundleId"`
	BundleVersion   string    `json:"bundleVersion"`
	Variant         string    `json:"variant"`
	Checks          []Check   `json:"checks"`
	CanInstall      bool      `json:"canInstall"`
	CanDownloadOnly bool      `json:"canDownloadOnly"`
	Plan            Plan      `json:"plan"`
	CheckedAt       time.Time `json:"checkedAt"`
}

// ClassifyHardware 把采集结果归入清单中的 hardwareClass。
func ClassifyHardware(hw HardwareSummary) string {
	if hw.Error != "" || len(hw.GPUs) == 0 {
		return "none"
	}
	names := make([]string, 0, len(hw.GPUs))
	for _, gpu := range hw.GPUs {
		names = append(names, strings.ToUpper(gpu.Name))
	}
	joined := strings.Join(names, " ")
	switch {
	case strings.Contains(joined, "GB10") || strings.Contains(strings.ToUpper(hw.HostModel), "SPARK"):
		return "spark-128g"
	case strings.Contains(joined, "PRO 6000") || strings.Contains(joined, "6000 BLACKWELL"):
		return "pro6000-96g"
	case strings.Contains(joined, "5090"):
		return "rtx5090-32g"
	case strings.Contains(joined, "4090"):
		return "rtx4090-24g"
	default:
		return "other-nvidia"
	}
}

// HardwareCheck 根据套装硬件规则给出匹配结论。
func HardwareCheck(b *Bundle, hw HardwareSummary) Check {
	class := ClassifyHardware(hw)
	check := Check{Code: "hardware", Title: "硬件匹配", CheckedAt: time.Now().UTC()}
	if hw.Error != "" {
		check.Status = CheckUnknown
		check.Detail = "硬件采集失败，无法评估运行环境：" + hw.Error
		check.Remediation = "可继续仅下载资产；关联实例前请重新采集硬件"
		check.Observed = "采集失败"
		return check
	}
	if class == "none" {
		check.Status = CheckUnknown
		check.Detail = "未检测到 NVIDIA GPU，无法评估运行环境"
		check.Remediation = "允许仅下载资产；关联实例前需在目标主机重新采集"
		check.Observed = "无 GPU"
		return check
	}
	for _, rule := range b.HardwareRules {
		if rule.HardwareClass != class {
			continue
		}
		check.Observed = describeHardware(class, hw)
		check.Required = b.Name + " 的候选硬件"
		check.Detail = rule.Note
		switch rule.Level {
		case LevelPriority, LevelCandidate:
			check.Status = CheckPass
		case LevelConditional, LevelExperimental:
			check.Status = CheckWarn
			check.Remediation = "可继续安装，但需在目标硬件完成人工验收后再对外承诺性能"
		case LevelNotRecommended, LevelNotCommitted:
			check.Status = CheckWarn
			check.Remediation = "允许下载，但不建议按标准交付或承诺 SLA"
		default:
			check.Status = CheckUnknown
		}
		return check
	}
	check.Status = CheckUnknown
	check.Observed = describeHardware(class, hw)
	check.Detail = "当前硬件不在该套装的候选矩阵内"
	check.Remediation = "可继续仅下载，运行前需人工评估"
	return check
}

func describeHardware(class string, hw HardwareSummary) string {
	if len(hw.GPUs) == 0 {
		return class
	}
	parts := make([]string, 0, len(hw.GPUs))
	for _, gpu := range hw.GPUs {
		parts = append(parts, fmt.Sprintf("#%d %s", gpu.Index, gpu.Name))
	}
	if hw.UnifiedMemory {
		parts = append(parts, "统一内存")
	}
	return strings.Join(parts, "；")
}

// MirrorCheck 检查 ComfyUI Core 与模板包版本；未知不当作通过。
func MirrorCheck(b *Bundle, coreVersion, templateVersion string) Check {
	check := Check{Code: "comfyui", Title: "镜像与版本", CheckedAt: time.Now().UTC()}
	check.Required = "ComfyUI " + b.RequiredComfyUI + "；模板包 " + b.RequiredTemplatePack + "+"
	if strings.TrimSpace(coreVersion) == "" {
		check.Status = CheckUnknown
		check.Detail = "镜像缺少 ComfyUI 版本元数据，无法确认"
		check.Observed = "未记录"
		check.Remediation = "可在镜像中心重新构建带版本元数据的镜像"
		return check
	}
	if !versionSatisfies(coreVersion, b.RequiredComfyUI) {
		check.Status = CheckBlock
		check.Observed = coreVersion
		check.Detail = "当前镜像 ComfyUI 版本低于套装要求"
		check.Remediation = "前往镜像中心构建满足要求的镜像；本版不自动触发构建"
		return check
	}
	if strings.TrimSpace(templateVersion) == "" {
		check.Status = CheckUnknown
		check.Observed = "Core " + coreVersion + "；模板包未记录"
		check.Detail = "Core 版本满足，但模板包与 H3 节点版本无法确认"
		check.Remediation = "安装后在 ComfyUI 中人工确认模板与节点"
		return check
	}
	if !versionSatisfies(templateVersion, b.RequiredTemplatePack+"+") {
		check.Status = CheckWarn
		check.Observed = "Core " + coreVersion + "；模板包 " + templateVersion
		check.Detail = "模板包版本可能不满足要求"
		check.Remediation = "升级 ComfyUI 模板包后重新预检"
		return check
	}
	check.Status = CheckPass
	check.Observed = "Core " + coreVersion + "；模板包 " + templateVersion
	check.Detail = "Core 与模板包版本满足要求；仍需人工确认 H3 节点与工作流"
	return check
}

// DiskCheck 计算缺失文件所需空间，已存在文件不重复计入。
func DiskCheck(b *Bundle, snapshot RepoSnapshot, availableBytes int64) (Check, int64) {
	check := Check{Code: "disk", Title: "磁盘空间", CheckedAt: time.Now().UTC()}
	var needed, reusable int64
	for _, item := range b.Items {
		expected := item.Sources[0]
		if size, exists := snapshot.Sizes[item.TargetRelativePath]; exists && size == expected.SizeBytes {
			reusable += expected.SizeBytes
			continue
		}
		needed += expected.SizeBytes
	}
	// 预留 5% 安全余量，避免下载到满盘。
	reserve := needed / 20
	required := needed + reserve
	check.Observed = "可用 " + formatBytes(availableBytes) + "；需下载 " + formatBytes(needed)
	check.Required = formatBytes(required) + "（含 5% 余量）"
	if reusable > 0 {
		check.Detail = "可复用已有文件 " + formatBytes(reusable)
	} else {
		check.Detail = "全部文件需下载"
	}
	switch {
	case availableBytes <= 0:
		check.Status = CheckUnknown
		check.Detail += "；无法读取可用空间"
	case required > availableBytes:
		check.Status = CheckBlock
		check.Detail += fmt.Sprintf("；还差 %s", formatBytes(required-availableBytes))
		check.Remediation = "清理不需要的文件或更换目标仓库后重试"
	default:
		check.Status = CheckPass
	}
	return check, needed
}

// LicenseCheck 依据后端记录判断，不信任前端声明。
func LicenseCheck(b *Bundle, confirmed bool) Check {
	check := Check{Code: "license", Title: "许可确认", CheckedAt: time.Now().UTC()}
	if !b.License.Configured() {
		check.Status = CheckPass
		check.Observed = "该模型未配置额外许可"
		check.Detail = "无需确认，可直接下载"
		return check
	}
	check.Required = b.License.Name + "（版本 " + b.License.Version + "）"
	if confirmed {
		check.Status = CheckPass
		check.Observed = b.License.ModelName + " 许可已确认"
		check.Detail = "确认仅表示你已阅读该许可"
		return check
	}
	check.Status = CheckBlock
	check.Observed = b.License.ModelName + " 许可待确认"
	check.Detail = "下载前需要先阅读并确认 " + b.License.ModelName + " 的许可"
	check.Remediation = "点击“确认并继续下载”即可继续"
	return check
}

// Preflight 汇总四项预检；磁盘不足或许可未确认会阻止安装，但允许仅下载许可允许时仍阻断。
func Preflight(b *Bundle, plan Plan, hw HardwareSummary, coreVersion, templateVersion string, snapshot RepoSnapshot, availableBytes int64, licenseConfirmed bool) PreflightResult {
	result := PreflightResult{
		BundleID: b.ID, BundleVersion: plan.BundleVersion, Variant: plan.Variant,
		Checks: []Check{
			HardwareCheck(b, hw),
			MirrorCheck(b, coreVersion, templateVersion),
			{}, // 磁盘占位，下面替换以带回 needed
			LicenseCheck(b, licenseConfirmed),
		},
		Plan: plan, CheckedAt: time.Now().UTC(),
	}
	diskCheck, _ := DiskCheck(b, snapshot, availableBytes)
	result.Checks[2] = diskCheck

	blocked := false
	for _, check := range result.Checks {
		if check.Status == CheckBlock {
			blocked = true
		}
	}
	result.CanInstall = !blocked
	result.CanDownloadOnly = diskCheck.Status != CheckBlock
	return result
}

// versionSatisfies 支持 ">=0.45.0"、"0.11.104+" 与裸版本号。
func versionSatisfies(actual, requirement string) bool {
	requirement = strings.TrimSpace(requirement)
	if requirement == "" {
		return true
	}
	operator := ">="
	switch {
	case strings.HasPrefix(requirement, ">="):
		requirement = strings.TrimSpace(requirement[2:])
	case strings.HasPrefix(requirement, ">"):
		operator = ">"
		requirement = strings.TrimSpace(requirement[1:])
	case strings.HasSuffix(requirement, "+"):
		requirement = strings.TrimSuffix(requirement, "+")
	}
	requirement = strings.TrimPrefix(requirement, "v")
	actual = strings.TrimPrefix(strings.TrimSpace(actual), "v")
	actualParts, okActual := parseVersion(actual)
	requiredParts, okRequired := parseVersion(requirement)
	if !okActual || !okRequired {
		return false
	}
	comparison := compareVersions(actualParts, requiredParts)
	if operator == ">" {
		return comparison > 0
	}
	return comparison >= 0
}

func parseVersion(value string) ([]int, bool) {
	// 去掉构建元数据与预发布后缀，只比较主版本。
	if index := strings.IndexAny(value, "-+"); index >= 0 {
		value = value[:index]
	}
	if value == "" {
		return nil, false
	}
	segments := strings.Split(value, ".")
	parts := make([]int, 0, len(segments))
	for _, segment := range segments {
		number, err := strconv.Atoi(segment)
		if err != nil {
			return nil, false
		}
		parts = append(parts, number)
	}
	return parts, true
}

func compareVersions(a, b []int) int {
	length := len(a)
	if len(b) > length {
		length = len(b)
	}
	for index := 0; index < length; index++ {
		left, right := 0, 0
		if index < len(a) {
			left = a[index]
		}
		if index < len(b) {
			right = b[index]
		}
		if left != right {
			if left < right {
				return -1
			}
			return 1
		}
	}
	return 0
}

func formatBytes(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	const unit = 1024
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	value := float64(bytes)
	index := 0
	for value >= unit && index < len(units)-1 {
		value /= unit
		index++
	}
	if index == 0 {
		return fmt.Sprintf("%d B", bytes)
	}
	return fmt.Sprintf("%.1f %s", value, units[index])
}
