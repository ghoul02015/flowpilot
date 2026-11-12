package bundle

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidationError 指出清单中具体套装与字段的问题，避免启动时静默跳过。
type ValidationError struct {
	BundleID string
	Field    string
	Message  string
}

func (e *ValidationError) Error() string {
	prefix := ""
	if e.BundleID != "" {
		prefix = "套装 " + e.BundleID + " "
	}
	return prefix + e.Field + "：" + e.Message
}

func invalid(bundleID, field, format string, args ...any) error {
	return &ValidationError{BundleID: bundleID, Field: field, Message: fmt.Sprintf(format, args...)}
}

// allowedCategories 与 model 包 categoryTitles 保持一致，是受控模型目录白名单。
var allowedCategories = map[string]bool{
	"checkpoints":      true,
	"loras":            true,
	"vae":              true,
	"controlnet":       true,
	"embeddings":       true,
	"upscale_models":   true,
	"text_encoders":    true,
	"diffusion_models": true,
	"clip_vision":      true,
	"model_patches":    true,
}

var (
	sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	// revision 必须是不可变提交，禁止浮动分支。
	revisionPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
	floatingRefs    = map[string]bool{"main": true, "master": true, "head": true, "dev": true, "latest": true}
)

// Validate 执行清单级与套装级严格校验。
func (m *Manifest) Validate() error {
	if m.ManifestVersion != DefaultManifestVersion {
		return fmt.Errorf("清单版本不支持：%s（期望 %s）", m.ManifestVersion, DefaultManifestVersion)
	}
	if strings.TrimSpace(m.BundleVersion) == "" {
		return fmt.Errorf("bundleVersion 不能为空")
	}
	if err := validateRelativePath(m.RepositoryRoot); err != nil {
		return fmt.Errorf("repositoryRoot 非法：%w", err)
	}
	for index, source := range m.Sources {
		if strings.TrimSpace(source.Repo) == "" || !strings.Contains(source.Repo, "/") {
			return fmt.Errorf("sources[%d].repo 非法：%q", index, source.Repo)
		}
		if err := validateRevision(source.Revision); err != nil {
			return fmt.Errorf("sources[%d]（%s）revision 非法：%w", index, source.Repo, err)
		}
	}
	if len(m.Bundles) == 0 {
		return fmt.Errorf("清单未包含任何套装")
	}

	seenBundle := map[string]bool{}
	// 同一目标路径只允许一个内容哈希，跨套装共享也必须完全一致。
	pathHash := map[string]string{}
	for index := range m.Bundles {
		b := &m.Bundles[index]
		if seenBundle[b.ID] {
			return invalid(b.ID, "id", "重复的套装 ID")
		}
		seenBundle[b.ID] = true
		if err := b.validate(""); err != nil {
			return err
		}
		for _, item := range b.Items {
			hash := strings.ToLower(item.Sources[0].SHA256)
			if previous, ok := pathHash[item.TargetRelativePath]; ok && previous != hash {
				return invalid(b.ID, "items."+item.Key,
					"目标路径 %s 已存在不同内容哈希（%s ≠ %s），必须视为冲突", item.TargetRelativePath, previous[:12], hash[:12])
			}
			pathHash[item.TargetRelativePath] = hash
		}
	}
	return nil
}

func (b *Bundle) validate(_ string) error {
	if strings.TrimSpace(b.ID) == "" {
		return invalid("", "id", "不能为空")
	}
	if strings.TrimSpace(b.Version) == "" {
		return invalid(b.ID, "version", "不能为空")
	}
	if strings.TrimSpace(b.Name) == "" || strings.TrimSpace(b.ModelName) == "" {
		return invalid(b.ID, "name/modelName", "不能为空")
	}
	if len(b.Capability) == 0 {
		return invalid(b.ID, "capability", "至少声明一种能力")
	}
	if b.ReleaseChannel != "standard" && b.ReleaseChannel != "experimental" {
		return invalid(b.ID, "releaseChannel", "只允许 standard 或 experimental，实际 %q", b.ReleaseChannel)
	}
	if b.Experimental != (b.ReleaseChannel == "experimental") {
		return invalid(b.ID, "experimental", "必须与 releaseChannel 一致")
	}
	if err := b.validateVariants(); err != nil {
		return err
	}
	if err := b.validateItems(); err != nil {
		return err
	}
	if err := b.validateLicense(); err != nil {
		return err
	}
	for index, workflow := range b.Workflows {
		if !strings.HasPrefix(workflow.OfficialURL, "https://") {
			return invalid(b.ID, fmt.Sprintf("workflows[%d].officialURL", index), "必须是 https 官方链接")
		}
		if workflow.TemplateStatus != "unverified" && workflow.TemplateStatus != "verified" {
			return invalid(b.ID, fmt.Sprintf("workflows[%d].templateStatus", index), "只允许 unverified 或 verified")
		}
	}
	for index, cert := range b.CertificationByHardware {
		if cert.Status != "certified" {
			return invalid(b.ID, fmt.Sprintf("certificationByHardware[%d].status", index), "只允许 certified")
		}
		if strings.TrimSpace(cert.Evidence) == "" {
			return invalid(b.ID, fmt.Sprintf("certificationByHardware[%d].evidence", index), "认证必须提供证据引用")
		}
	}
	return nil
}

func (b *Bundle) validateVariants() error {
	if len(b.Variants) == 0 {
		return invalid(b.ID, "variants", "至少需要一个变体")
	}
	seen := map[string]bool{}
	defaults := 0
	groupDefaults := map[string]int{}
	for index, variant := range b.Variants {
		field := fmt.Sprintf("variants[%d]", index)
		if strings.TrimSpace(variant.Key) == "" {
			return invalid(b.ID, field+".key", "不能为空")
		}
		if seen[variant.Key] {
			return invalid(b.ID, field+".key", "重复的变体 key")
		}
		seen[variant.Key] = true
		if variant.Default {
			defaults++
			if variant.OptionalGroup != "" {
				groupDefaults[variant.OptionalGroup]++
			}
		}
		for _, key := range variant.Selects {
			item, ok := b.Item(key)
			if !ok {
				return invalid(b.ID, field+".selects", "引用了不存在的项 %s", key)
			}
			if item.Required {
				return invalid(b.ID, field+".selects", "变体只能选择可选项 %s", key)
			}
		}
	}
	if defaults == 0 {
		return invalid(b.ID, "variants", "至少需要一个默认变体")
	}
	for group, count := range groupDefaults {
		if count > 1 {
			return invalid(b.ID, "variants", "互斥组 %s 默认选中项超过一个", group)
		}
	}
	return nil
}

func (b *Bundle) validateItems() error {
	if len(b.Items) == 0 {
		return invalid(b.ID, "items", "至少需要一个文件项")
	}
	keys := map[string]bool{}
	// 先收集全部 key，允许依赖项后置声明。
	for index := range b.Items {
		item := &b.Items[index]
		if strings.TrimSpace(item.Key) == "" {
			return invalid(b.ID, fmt.Sprintf("items[%d].key", index), "不能为空")
		}
		if keys[item.Key] {
			return invalid(b.ID, fmt.Sprintf("items[%d].key", index), "重复的文件项 key")
		}
		keys[item.Key] = true
	}
	for index := range b.Items {
		item := &b.Items[index]
		field := fmt.Sprintf("items[%d]", index)
		if !allowedCategories[item.Category] {
			return invalid(b.ID, field+".category", "不在受控模型目录内：%q", item.Category)
		}
		if err := validateRelativePath(item.TargetRelativePath); err != nil {
			return invalid(b.ID, field+".targetRelativePath", "%v", err)
		}
		if !strings.HasPrefix(item.TargetRelativePath, item.Category+"/") {
			return invalid(b.ID, field+".targetRelativePath", "首段必须等于 category %q", item.Category)
		}
		if item.Required && item.OptionalGroup != "" {
			return invalid(b.ID, field+".optionalGroup", "必需项不能属于可选组")
		}
		if len(item.Sources) == 0 {
			return invalid(b.ID, field+".sources", "至少需要一个来源")
		}
		for sourceIndex, source := range item.Sources {
			sourceField := fmt.Sprintf("%s.sources[%d]", field, sourceIndex)
			if strings.TrimSpace(source.Repo) == "" || !strings.Contains(source.Repo, "/") {
				return invalid(b.ID, sourceField+".repo", "必须是 owner/name 形式")
			}
			if err := validateRevision(source.Revision); err != nil {
				return invalid(b.ID, sourceField+".revision", "%v", err)
			}
			if source.SizeBytes <= 0 {
				return invalid(b.ID, sourceField+".sizeBytes", "必须为正整数")
			}
			if !sha256Pattern.MatchString(strings.ToLower(source.SHA256)) {
				return invalid(b.ID, sourceField+".sha256", "必须是 64 位十六进制")
			}
		}
		for _, dependency := range item.DependsOn {
			if !keys[dependency] {
				return invalid(b.ID, field+".dependsOn", "依赖项 %s 不存在", dependency)
			}
			if dependency == item.Key {
				return invalid(b.ID, field+".dependsOn", "不能依赖自身")
			}
		}
	}
	return b.validateDependencyGraph()
}

// validateDependencyGraph 检测依赖环。
func (b *Bundle) validateDependencyGraph() error {
	state := map[string]int{} // 0 未访问，1 访问中，2 完成
	var walk func(key string, path []string) error
	walk = func(key string, path []string) error {
		switch state[key] {
		case 1:
			return invalid(b.ID, "items."+key+".dependsOn", "存在循环依赖：%s", strings.Join(append(path, key), " → "))
		case 2:
			return nil
		}
		state[key] = 1
		item, ok := b.Item(key)
		if !ok {
			return invalid(b.ID, "items."+key, "依赖引用了不存在的项")
		}
		for _, dependency := range item.DependsOn {
			if err := walk(dependency, append(path, key)); err != nil {
				return err
			}
		}
		state[key] = 2
		return nil
	}
	for _, item := range b.Items {
		if err := walk(item.Key, nil); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bundle) validateLicense() error {
	license := b.License
	if strings.TrimSpace(license.Name) == "" || strings.TrimSpace(license.SourceURL) == "" {
		return invalid(b.ID, "license", "名称与原始链接不能为空")
	}
	if !strings.HasPrefix(license.SourceURL, "https://") {
		return invalid(b.ID, "license.sourceURL", "必须是 https 链接")
	}
	if strings.TrimSpace(license.AcknowledgementVersion) == "" {
		return invalid(b.ID, "license.acknowledgementVersion", "不能为空")
	}
	switch license.ReviewStatus {
	case "needs_review", "approved", "rejected":
	default:
		return invalid(b.ID, "license.reviewStatus", "只允许 needs_review／approved／rejected，实际 %q", license.ReviewStatus)
	}
	return nil
}

func validateRevision(revision string) error {
	trimmed := strings.ToLower(strings.TrimSpace(revision))
	if trimmed == "" {
		return fmt.Errorf("不能为空")
	}
	if floatingRefs[trimmed] {
		return fmt.Errorf("不允许浮动引用 %q，必须锁定提交 SHA", revision)
	}
	if !revisionPattern.MatchString(trimmed) {
		return fmt.Errorf("必须是 40 位提交 SHA，实际 %q", revision)
	}
	return nil
}

// validateRelativePath 拒绝绝对路径、回溯、反斜杠与空段。
func validateRelativePath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("路径为空")
	}
	if strings.ContainsRune(path, 0) {
		return fmt.Errorf("路径包含非法字符")
	}
	if strings.Contains(path, "\\") {
		return fmt.Errorf("路径不允许反斜杠：%q", path)
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "~") {
		return fmt.Errorf("不允许绝对路径：%q", path)
	}
	if strings.Contains(path, "://") {
		return fmt.Errorf("不允许 URL 形式路径：%q", path)
	}
	for _, segment := range strings.Split(path, "/") {
		switch segment {
		case "":
			return fmt.Errorf("路径含空段：%q", path)
		case ".", "..":
			return fmt.Errorf("路径不允许回溯段：%q", path)
		}
	}
	return nil
}
