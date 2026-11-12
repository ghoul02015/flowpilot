package bundle

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed assets/minimax-h3-bundles/v1/bundles.json
var defaultManifestJSON []byte

//go:embed assets/licenses
var licenseAssets embed.FS

// DefaultManifestVersion 是随本版二进制固定的清单版本。
const DefaultManifestVersion = "minimax-h3-bundles/v1"

// Default 加载并校验内置清单。
func Default() (*Manifest, error) {
	return Parse(defaultManifestJSON)
}

// LicenseAsset 读取内置许可文件（清单里以 licenses/... 相对路径引用）。
func LicenseAsset(path string) ([]byte, error) {
	return licenseAssets.ReadFile("assets/" + path)
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
	if err := manifest.attachLicenses(); err != nil {
		return nil, err
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	return &manifest, nil
}

// attachLicenses 把模型级许可挂到套装上，并校验内置快照未被改动。
func (m *Manifest) attachLicenses() error {
	for index := range m.Bundles {
		b := &m.Bundles[index]
		license, ok := m.Licenses[b.ModelName]
		if !ok {
			return fmt.Errorf("套装 %s 使用的模型 %s 未配置许可", b.ID, b.ModelName)
		}
		if license.ModelName == "" {
			license.ModelName = b.ModelName
		}
		if license.SourceURL == "" {
			license.SourceURL = license.OfficialURL
		}
		if license.AcknowledgementVersion == "" {
			license.AcknowledgementVersion = license.Version
		}
		if err := verifyLicenseSnapshot(license); err != nil {
			return fmt.Errorf("套装 %s：%w", b.ID, err)
		}
		b.License = license
	}
	return nil
}

// verifyLicenseSnapshot 确认内置 LICENSE／NOTICE 与清单声明一致，
// 防止清单改了版本号而文件未更新的“假快照”。
func verifyLicenseSnapshot(license License) error {
	data, err := LicenseAsset(license.LicenseFile)
	if err != nil {
		return fmt.Errorf("缺少许可文件 %s：%w", license.LicenseFile, err)
	}
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, license.SnapshotSHA256) {
		return fmt.Errorf("许可快照校验失败：%s 期望 %s，实际 %s", license.LicenseFile, license.SnapshotSHA256, actual)
	}
	notice, err := LicenseAsset(license.NoticeFile)
	if err != nil {
		return fmt.Errorf("缺少 NOTICE 文件 %s：%w", license.NoticeFile, err)
	}
	if strings.TrimSpace(string(notice)) != strings.TrimSpace(license.NoticeText) {
		return fmt.Errorf("NOTICE 文件内容与清单不一致：%s", license.NoticeFile)
	}
	return nil
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
