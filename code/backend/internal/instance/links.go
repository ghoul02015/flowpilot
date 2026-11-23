package instance

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ModelLink 是实例关联的一个共享仓库模型。
// Category 是仓库第一层子目录（ComfyUI 模型类别），Name 是类别目录下的相对路径，
// 支持子目录（如 checkpoints/sd15/a.safetensors 的 Name 为 sd15/a.safetensors）。
type ModelLink struct {
	Category string `json:"category"`
	Name     string `json:"name"`
}

// NotSupportedError 表示实例由旧版本创建、缺少模型关联挂载。
// Docker 的 bind 挂载在容器创建时固定，重启不会补加，只能重建。
type NotSupportedError struct{ Name string }

func (e *NotSupportedError) Error() string {
	return fmt.Sprintf("实例 %s 由旧版本创建，缺少模型关联挂载，需删除并以相同参数重建后使用（模型、输出与工作流均在共享目录，重建无损）", e.Name)
}

var linkCategoryPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// ListModelLinks 返回实例是否支持模型关联及当前关联列表。
func (m *Manager) ListModelLinks(ctx context.Context, name string) (bool, []ModelLink, error) {
	target, err := m.Get(ctx, name)
	if err != nil {
		return false, nil, err
	}
	links := make([]ModelLink, 0)
	if !target.ModelLinks || m.linksContainerDir == "" {
		return false, links, nil
	}
	root := filepath.Join(m.linksContainerDir, linkDirName(target))
	categories, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return true, links, nil
		}
		return true, links, fmt.Errorf("读取模型关联目录失败：%w", err)
	}
	for _, category := range categories {
		if !category.IsDir() {
			continue
		}
		categoryRoot := filepath.Join(root, category.Name())
		walkErr := filepath.WalkDir(categoryRoot, func(p string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.Type()&os.ModeSymlink == 0 {
				return nil
			}
			relative, relErr := filepath.Rel(categoryRoot, p)
			if relErr != nil {
				return nil
			}
			links = append(links, ModelLink{Category: category.Name(), Name: filepath.ToSlash(relative)})
			return nil
		})
		if walkErr != nil {
			continue
		}
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].Category != links[j].Category {
			return links[i].Category < links[j].Category
		}
		return links[i].Name < links[j].Name
	})
	return true, links, nil
}

// SetModelLinksFor 以全量集合语义设置实例的模型关联：
// 缺失的创建符号链接、多余的移除，返回新增/移除数量。
// 符号链接指向「实例容器内」的模型路径（链接只在实例容器内解析，
// 宿主机侧看到断链属预期）；增删后 ComfyUI 刷新页面即可见，无需重启实例。
func (m *Manager) SetModelLinksFor(ctx context.Context, name string, want []ModelLink) (int, int, error) {
	target, err := m.Get(ctx, name)
	if err != nil {
		return 0, 0, err
	}
	if !target.ModelLinks || m.linksContainerDir == "" {
		return 0, 0, &NotSupportedError{Name: name}
	}

	// 校验并按类别+相对路径去重。
	desired := make(map[string]ModelLink, len(want))
	for _, link := range want {
		category := strings.TrimSpace(link.Category)
		if !linkCategoryPattern.MatchString(category) {
			return 0, 0, &ValidationError{Message: fmt.Sprintf("模型类别不合法：%s", link.Category)}
		}
		relative := normalizeLinkName(link.Name)
		if relative == "" {
			return 0, 0, &ValidationError{Message: fmt.Sprintf("模型文件名不合法：%s", link.Name)}
		}
		modelPath := filepath.Join(append([]string{m.modelRoot, category}, strings.Split(relative, "/")...)...)
		info, statErr := os.Stat(modelPath)
		if statErr != nil || !info.Mode().IsRegular() {
			return 0, 0, &ValidationError{Message: fmt.Sprintf("共享仓库中不存在模型：%s/%s", category, relative)}
		}
		desired[category+"/"+relative] = ModelLink{Category: category, Name: relative}
	}

	m.linksMu.Lock()
	defer m.linksMu.Unlock()

	root := filepath.Join(m.linksContainerDir, linkDirName(target))
	existing := make(map[string]bool)
	_ = filepath.WalkDir(root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil || entry.Type()&os.ModeSymlink == 0 {
			return nil
		}
		relative, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		existing[filepath.ToSlash(relative)] = true
		return nil
	})

	added, removed := 0, 0
	for key, link := range desired {
		if existing[key] {
			delete(existing, key)
			continue
		}
		linkPath := filepath.Join(append([]string{root, link.Category}, strings.Split(link.Name, "/")...)...)
		if mkErr := os.MkdirAll(filepath.Dir(linkPath), 0o755); mkErr != nil {
			return added, removed, fmt.Errorf("创建关联目录失败：%w", mkErr)
		}
		// 链接目标必须是实例容器内路径（如 /data/models/loras/x.safetensors）。
		containerTarget := strings.TrimRight(target.ModelsTarget, "/") + "/" + link.Category + "/" + link.Name
		if createErr := os.Symlink(containerTarget, linkPath); createErr != nil {
			return added, removed, fmt.Errorf("创建模型关联失败：%w", createErr)
		}
		added++
	}
	for key := range existing {
		linkPath := filepath.Join(append([]string{root}, strings.Split(key, "/")...)...)
		if removeErr := os.Remove(linkPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return added, removed, fmt.Errorf("移除模型关联失败：%w", removeErr)
		}
		removed++
		pruneEmptyDirs(filepath.Dir(linkPath), root)
	}
	return added, removed, nil
}

// normalizeLinkName 规范化类别目录下的相对路径；
// 拒绝绝对路径、`.`/`..` 与路径穿越，返回空字符串表示不合法。
func normalizeLinkName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	cleaned := path.Clean(filepath.ToSlash(trimmed))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return ""
	}
	if strings.ContainsAny(cleaned, "\x00") {
		return ""
	}
	return cleaned
}

// pruneEmptyDirs 自底向上删除空目录，直到停在上限 root（不含）。
func pruneEmptyDirs(dir, root string) {
	for {
		cleanedDir := filepath.Clean(dir)
		cleanedRoot := filepath.Clean(root)
		if cleanedDir == cleanedRoot || !strings.HasPrefix(cleanedDir, cleanedRoot+string(filepath.Separator)) {
			return
		}
		entries, err := os.ReadDir(cleanedDir)
		if err != nil || len(entries) > 0 {
			return
		}
		if removeErr := os.Remove(cleanedDir); removeErr != nil {
			return
		}
		dir = filepath.Dir(cleanedDir)
	}
}

// linkDirName 模型链接目录名：0.0.9.7 起新实例用稳定 UUID，旧实例沿用名称。
func linkDirName(target Instance) string {
	if target.UUID != "" {
		return target.UUID
	}
	return target.Name
}
