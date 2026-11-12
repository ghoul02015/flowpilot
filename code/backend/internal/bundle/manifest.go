package bundle

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

//go:embed assets/minimax-h3-bundles/v1/bundles.json
var defaultManifestJSON []byte

// DefaultManifestVersion 是随本版二进制固定的清单版本。
const DefaultManifestVersion = "minimax-h3-bundles/v1"

// Default 加载并校验内置清单。
func Default() (*Manifest, error) {
	return Parse(defaultManifestJSON)
}

// Parse 解析并严格校验清单；未知字段视为错误，避免拼写错误被静默忽略。
func Parse(raw []byte) (*Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("套装清单解析失败：%w", err)
	}
	sum := sha256.Sum256(raw)
	manifest.Checksum = hex.EncodeToString(sum[:])
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	return &manifest, nil
}

// FindBundle 按 ID 查找套装。
func (m *Manifest) FindBundle(id string) (*Bundle, bool) {
	for index := range m.Bundles {
		if m.Bundles[index].ID == id {
			return &m.Bundles[index], true
		}
	}
	return nil, false
}

// Item 按 key 查找套装文件项。
func (b *Bundle) Item(key string) (*Item, bool) {
	for index := range b.Items {
		if b.Items[index].Key == key {
			return &b.Items[index], true
		}
	}
	return nil, false
}

// RequiredBytes 返回必需项字节数（同一文件只计一次）。
func (b *Bundle) RequiredBytes() int64 {
	return b.bytesOf(func(item Item) bool { return item.Required })
}

// TotalBytes 返回全部项字节数（含可选项，同一文件只计一次）。
func (b *Bundle) TotalBytes() int64 {
	return b.bytesOf(func(Item) bool { return true })
}

func (b *Bundle) bytesOf(pick func(Item) bool) int64 {
	seen := map[string]bool{}
	var total int64
	for _, item := range b.Items {
		if !pick(item) || seen[item.TargetRelativePath] || len(item.Sources) == 0 {
			continue
		}
		seen[item.TargetRelativePath] = true
		total += item.Sources[0].SizeBytes
	}
	return total
}

// RequiredItemCount 返回必需文件项数量。
func (b *Bundle) RequiredItemCount() int {
	count := 0
	for _, item := range b.Items {
		if item.Required {
			count++
		}
	}
	return count
}

// OptionalItemCount 返回可选文件项数量。
func (b *Bundle) OptionalItemCount() int {
	return len(b.Items) - b.RequiredItemCount()
}
