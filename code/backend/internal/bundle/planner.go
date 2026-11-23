package bundle

import (
	"fmt"
	"sort"
)

// PlanItem 是安装快照中的一个文件项。
type PlanItem struct {
	Key                string `json:"key"`
	Role               string `json:"role"`
	Required           bool   `json:"required"`
	Ordinal            int    `json:"ordinal"`
	Category           string `json:"category"`
	TargetRelativePath string `json:"targetRelativePath"`
	Repo               string `json:"repo"`
	Revision           string `json:"revision"`
	FilePath           string `json:"filePath"`
	SizeBytes          int64  `json:"sizeBytes"`
	SHA256             string `json:"sha256"`
}

// ExcludedItem 记录被互斥规则排除的可选项，用于向用户解释。
type ExcludedItem struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// Plan 是不可变的安装快照：worker 只按快照执行，不随后续清单变化。
type Plan struct {
	BundleID               string         `json:"bundleId"`
	BundleVersion          string         `json:"bundleVersion"`
	ManifestChecksum       string         `json:"manifestChecksum"`
	Variant                string         `json:"variant"`
	SelectedOptions        []string       `json:"selectedOptions"`
	Items                  []PlanItem     `json:"items"`
	Excluded               []ExcludedItem `json:"excluded"`
	RequiredBytes          int64          `json:"requiredBytes"`
	OptionalBytes          int64          `json:"optionalBytes"`
	TotalBytes             int64          `json:"totalBytes"`
	RequiredComfyUI        string         `json:"requiredComfyUI"`
	RequiredTemplatePack   string         `json:"requiredTemplatePack"`
	AcknowledgementVersion string         `json:"acknowledgementVersion"`
}

// Expand 按变体与选项展开安装快照。纯函数，不访问网络与磁盘。
func Expand(manifest *Manifest, bundleID, variantKey string, selectedOptionalKeys []string) (Plan, error) {
	b, ok := manifest.FindBundle(bundleID)
	if !ok {
		return Plan{}, &ValidationError{BundleID: bundleID, Field: "id", Message: "套装不存在"}
	}
	variant, ok := findVariant(b, variantKey)
	if !ok {
		if variantKey == "" {
			for _, candidate := range b.Variants {
				if candidate.Default {
					variant = candidate
					ok = true
					break
				}
			}
		}
		if !ok {
			return Plan{}, &ValidationError{BundleID: bundleID, Field: "variant", Message: "变体不存在：" + variantKey}
		}
	}

	// 合并变体自带选项与用户显式选择。
	selected := map[string]bool{}
	for _, key := range variant.Selects {
		selected[key] = true
	}
	for _, key := range selectedOptionalKeys {
		item, exists := b.Item(key)
		if !exists {
			return Plan{}, &ValidationError{BundleID: bundleID, Field: "selectedOptions", Message: "选项不存在：" + key}
		}
		if item.Required {
			return Plan{}, &ValidationError{BundleID: bundleID, Field: "selectedOptions", Message: "必需项无需选择：" + key}
		}
		selected[key] = true
	}

	// 互斥组：同组至多一个被选中。
	chosenByGroup := map[string]string{}
	for key := range selected {
		item, _ := b.Item(key)
		if item.OptionalGroup == "" {
			continue
		}
		if existing, conflict := chosenByGroup[item.OptionalGroup]; conflict && existing != key {
			return Plan{}, &ValidationError{
				BundleID: bundleID,
				Field:    "selectedOptions",
				Message:  fmt.Sprintf("互斥组 %s 只能选择一个（%s 与 %s）", item.OptionalGroup, existing, key),
			}
		}
		chosenByGroup[item.OptionalGroup] = key
	}

	// 依赖闭包：选中项依赖的可选项需要一并加入。
	include := map[string]bool{}
	queue := []string{}
	for _, item := range b.Items {
		if item.Required {
			include[item.Key] = true
			queue = append(queue, item.Key)
		}
	}
	for key := range selected {
		if !include[key] {
			include[key] = true
			queue = append(queue, key)
		}
	}
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		item, ok := b.Item(key)
		if !ok {
			continue
		}
		for _, dependency := range item.DependsOn {
			if include[dependency] {
				continue
			}
			dep, exists := b.Item(dependency)
			if !exists {
				return Plan{}, &ValidationError{BundleID: bundleID, Field: "items." + key, Message: "依赖项不存在：" + dependency}
			}
			// 依赖项若属互斥组且该组已选其他项，则视为冲突。
			if dep.OptionalGroup != "" {
				if other, conflict := chosenByGroup[dep.OptionalGroup]; conflict && other != dependency {
					return Plan{}, &ValidationError{
						BundleID: bundleID,
						Field:    "selectedOptions",
						Message:  fmt.Sprintf("选项 %s 依赖 %s，但与互斥组 %s 中已选的 %s 冲突", key, dependency, dep.OptionalGroup, other),
					}
				}
				chosenByGroup[dep.OptionalGroup] = dependency
			}
			include[dependency] = true
			queue = append(queue, dependency)
		}
	}

	plan := Plan{
		BundleID:               b.ID,
		BundleVersion:          manifest.BundleVersion,
		ManifestChecksum:       manifest.Checksum,
		Variant:                variant.Key,
		SelectedOptions:        []string{},
		Items:                  []PlanItem{},
		Excluded:               []ExcludedItem{},
		RequiredComfyUI:        b.RequiredComfyUI,
		RequiredTemplatePack:   b.RequiredTemplatePack,
		AcknowledgementVersion: b.License.AcknowledgementVersion,
	}

	// 按清单顺序稳定输出，同一目标路径只保留一次。
	seenPath := map[string]bool{}
	for _, item := range b.Items {
		selectedOptional := !item.Required && selected[item.Key]
		if !item.Required && !include[item.Key] {
			if item.OptionalGroup != "" {
				if other, taken := chosenByGroup[item.OptionalGroup]; taken && other != item.Key {
					plan.Excluded = append(plan.Excluded, ExcludedItem{Key: item.Key, Reason: "互斥组 " + item.OptionalGroup + " 已选择 " + other})
				}
			}
			continue
		}
		if seenPath[item.TargetRelativePath] {
			continue
		}
		seenPath[item.TargetRelativePath] = true
		source := item.Sources[0]
		plan.Items = append(plan.Items, PlanItem{
			Key:                item.Key,
			Role:               item.Role,
			Required:           item.Required,
			Ordinal:            len(plan.Items) + 1,
			Category:           item.Category,
			TargetRelativePath: item.TargetRelativePath,
			Repo:               source.Repo,
			Revision:           source.Revision,
			FilePath:           source.FilePath,
			SizeBytes:          source.SizeBytes,
			SHA256:             source.SHA256,
		})
		if item.Required {
			plan.RequiredBytes += source.SizeBytes
		} else if selectedOptional {
			plan.OptionalBytes += source.SizeBytes
		} else {
			// 依赖闭包带入的可选项也计入可选字节。
			plan.OptionalBytes += source.SizeBytes
		}
		if !item.Required {
			plan.SelectedOptions = append(plan.SelectedOptions, item.Key)
		}
	}
	plan.TotalBytes = plan.RequiredBytes + plan.OptionalBytes
	sort.Strings(plan.SelectedOptions)
	return plan, nil
}

func findVariant(b *Bundle, key string) (Variant, bool) {
	for _, variant := range b.Variants {
		if variant.Key == key {
			return variant, true
		}
	}
	return Variant{}, false
}
