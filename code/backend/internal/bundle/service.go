package bundle

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Service 汇聚套装列表、详情、预检、安装与验收，供 HTTP 层调用。
type Service struct {
	Manifest   *Manifest
	Store      *Store
	Installer  *Installer
	ScanRepo   func() (RepoSnapshot, int64, error)
	Hardware   func() HardwareSummary
	ImageMeta  func() (coreVersion, templateVersion string)
	LinkModels func(ctx context.Context, instanceName string, items []PlanItem) ([]string, error)
	Now        func() time.Time
	// Org 是确认记录归属的组织／租户；单机部署固定 default。
	Org string
	// Environment 记录确认发生的部署环境，便于审计。
	Environment string
}

// BundleCard 是列表页卡片数据。
type BundleCard struct {
	ID             string      `json:"id"`
	Version        string      `json:"version"`
	Name           string      `json:"name"`
	ModelName      string      `json:"modelName"`
	Summary        string      `json:"summary"`
	Capability     []string    `json:"capability"`
	ReleaseChannel string      `json:"releaseChannel"`
	Experimental   bool        `json:"experimental"`
	RequiredItems  int         `json:"requiredItems"`
	OptionalItems  int         `json:"optionalItems"`
	RequiredBytes  int64       `json:"requiredBytes"`
	TotalBytes     int64       `json:"totalBytes"`
	State          BundleState `json:"state"`
	HardwareClass  string      `json:"hardwareClass"`
	HardwareLevel  string      `json:"hardwareLevel"`
	HardwareNote   string      `json:"hardwareNote"`
	License        License     `json:"license"`
	// 用户侧只看“许可文本是否已确认”；内部审核状态单独展示，两者不混用。
	LicenseConfirmed   bool            `json:"licenseConfirmed"`
	LicenseConfirmedAt string          `json:"licenseConfirmedAt,omitempty"`
	LicenseConfirmedBy string          `json:"licenseConfirmedBy,omitempty"`
	Certification      []Certification `json:"certification"`
	ActiveInstall      *Install        `json:"activeInstall,omitempty"`
	LatestAcceptance   *Acceptance     `json:"latestAcceptance,omitempty"`
}

// BundleDetail 是详情抽屉数据。
type BundleDetail struct {
	Card        BundleCard      `json:"card"`
	Manifest    ManifestInfo    `json:"manifest"`
	Items       []ItemState     `json:"items"`
	Workflows   []WorkflowRef   `json:"workflows"`
	Hardware    HardwareSummary `json:"hardware"`
	Excluded    []ExcludedItem  `json:"excluded,omitempty"`
	Install     *Install        `json:"install,omitempty"`
	Acceptances []Acceptance    `json:"acceptances"`
	// LicenseConfirmations 是该模型的确认历史（最新在前）。
	LicenseConfirmations []LicenseConfirmation `json:"licenseConfirmations"`
}

// ManifestInfo 暴露清单版本与校验值，便于前端展示可追溯性。
type ManifestInfo struct {
	Version       string `json:"version"`
	BundleVersion string `json:"bundleVersion"`
	Checksum      string `json:"checksum"`
	GeneratedAt   string `json:"generatedAt"`
}

// CreateInstallRequest 是创建安装计划的入参。
type CreateInstallRequest struct {
	Variant         string   `json:"variant"`
	SelectedOptions []string `json:"selectedOptions"`
	RequestKey      string   `json:"requestKey"`
	Repository      string   `json:"repository"`
	Instance        string   `json:"instance"`
}

// 服务层错误。
var (
	ErrLicenseNotConfirmed = errors.New("许可尚未确认")
	ErrPreflightBlocked    = errors.New("预检未通过")
)

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

func (s *Service) org() string {
	if strings.TrimSpace(s.Org) != "" {
		return s.Org
	}
	return "default"
}

func (s *Service) environment() string {
	if strings.TrimSpace(s.Environment) != "" {
		return s.Environment
	}
	return "default"
}

// licenseState 查询某模型的确认状态并返回最近一条记录。
func (s *Service) licenseState(ctx context.Context, modelName, licenseVersion string) (bool, *LicenseConfirmation) {
	confirmed, err := s.Store.LicenseConfirmed(ctx, s.org(), modelName, licenseVersion)
	if err != nil {
		return false, nil
	}
	history, err := s.Store.ListLicenseConfirmations(ctx, s.org(), modelName, 1)
	if err != nil || len(history) == 0 {
		return confirmed, nil
	}
	return confirmed, &history[0]
}

func (s *Service) scan() (RepoSnapshot, int64, error) {
	if s.ScanRepo == nil {
		return NewRepoSnapshot("", nil), 0, nil
	}
	return s.ScanRepo()
}

// List 返回全部套装卡片；仓库只扫描一次。
func (s *Service) List(ctx context.Context) ([]BundleCard, error) {
	snapshot, _, err := s.scan()
	if err != nil {
		return nil, fmt.Errorf("扫描模型仓库失败：%w", err)
	}
	hardware := HardwareSummary{}
	if s.Hardware != nil {
		hardware = s.Hardware()
	}
	class := ClassifyHardware(hardware)
	cards := make([]BundleCard, 0, len(s.Manifest.Bundles))
	for index := range s.Manifest.Bundles {
		b := &s.Manifest.Bundles[index]
		state := ComputeState(b, snapshot, nil)
		card := BundleCard{
			ID: b.ID, Version: b.Version, Name: b.Name, ModelName: b.ModelName,
			Summary: b.Summary, Capability: b.Capability, ReleaseChannel: b.ReleaseChannel,
			Experimental: b.Experimental, RequiredItems: b.RequiredItemCount(),
			OptionalItems: b.OptionalItemCount(), RequiredBytes: b.RequiredBytes(),
			TotalBytes: b.TotalBytes(), State: state, HardwareClass: class,
			License: b.License, Certification: b.CertificationByHardware,
		}
		for _, rule := range b.HardwareRules {
			if rule.HardwareClass == class {
				card.HardwareLevel = rule.Level
				card.HardwareNote = rule.Note
				break
			}
		}
		if card.HardwareLevel == "" {
			card.HardwareLevel = LevelUnknown
		}
		if confirmed, latest := s.licenseState(ctx, b.ModelName, b.License.Version); confirmed {
			card.LicenseConfirmed = true
			if latest != nil {
				card.LicenseConfirmedAt = latest.ConfirmedAt.UTC().Format(time.RFC3339)
				card.LicenseConfirmedBy = latest.Operator
			}
		}
		// 活动计划优先覆盖状态（任意变体）。
		if active, found, _ := s.Store.FindActiveByBundle(ctx, b.ID, s.Manifest.BundleVersion, "models"); found {
			copied := active
			card.ActiveInstall = &copied
			state.OverlayInstallStatus(&active)
			card.State = state
		}
		if list, total, err := s.Store.ListAcceptances(ctx, b.ID, 1, 0); err == nil && total > 0 && len(list) > 0 {
			last := list[0]
			card.LatestAcceptance = &last
		}
		cards = append(cards, card)
	}
	return cards, nil
}

// Detail 返回套装详情，包含文件状态、安装与验收。
func (s *Service) Detail(ctx context.Context, id string) (BundleDetail, error) {
	b, ok := s.Manifest.FindBundle(id)
	if !ok {
		return BundleDetail{}, &ValidationError{BundleID: id, Field: "id", Message: "套装不存在"}
	}
	snapshot, _, err := s.scan()
	if err != nil {
		return BundleDetail{}, fmt.Errorf("扫描模型仓库失败：%w", err)
	}
	hardware := HardwareSummary{}
	if s.Hardware != nil {
		hardware = s.Hardware()
	}
	state := ComputeState(b, snapshot, nil)
	detail := BundleDetail{
		Card: BundleCard{
			ID: b.ID, Version: b.Version, Name: b.Name, ModelName: b.ModelName,
			Summary: b.Summary, Capability: b.Capability, ReleaseChannel: b.ReleaseChannel,
			Experimental: b.Experimental, RequiredItems: b.RequiredItemCount(),
			OptionalItems: b.OptionalItemCount(), RequiredBytes: b.RequiredBytes(),
			TotalBytes: b.TotalBytes(), State: state, License: b.License,
			Certification: b.CertificationByHardware,
		},
		Manifest: ManifestInfo{
			Version: s.Manifest.ManifestVersion, BundleVersion: s.Manifest.BundleVersion,
			Checksum: s.Manifest.Checksum, GeneratedAt: s.Manifest.GeneratedAt,
		},
		Items:     state.Items,
		Workflows: b.Workflows,
		Hardware:  hardware,
	}
	if confirmed, latest := s.licenseState(ctx, b.ModelName, b.License.Version); confirmed {
		detail.Card.LicenseConfirmed = true
		if latest != nil {
			detail.Card.LicenseConfirmedAt = latest.ConfirmedAt.UTC().Format(time.RFC3339)
			detail.Card.LicenseConfirmedBy = latest.Operator
		}
	}
	if history, err := s.Store.ListLicenseConfirmations(ctx, s.org(), b.ModelName, 10); err == nil {
		detail.LicenseConfirmations = history
	}
	if active, found, _ := s.Store.FindActiveByBundle(ctx, b.ID, s.Manifest.BundleVersion, "models"); found {
		copied := active
		detail.Install = &copied
		state.OverlayInstallStatus(&active)
		detail.Card.State = state
	}
	list, _, _ := s.Store.ListAcceptances(ctx, b.ID, 10, 0)
	detail.Acceptances = list
	return detail, nil
}

// Preflight 生成不产生写操作的预检结果。
func (s *Service) Preflight(ctx context.Context, id, variant string, options []string, operator string) (PreflightResult, error) {
	b, ok := s.Manifest.FindBundle(id)
	if !ok {
		return PreflightResult{}, &ValidationError{BundleID: id, Field: "id", Message: "套装不存在"}
	}
	plan, err := Expand(s.Manifest, id, variant, options)
	if err != nil {
		return PreflightResult{}, err
	}
	snapshot, available, _ := s.scan()
	hardware := HardwareSummary{}
	if s.Hardware != nil {
		hardware = s.Hardware()
	}
	core, template := "", ""
	if s.ImageMeta != nil {
		core, template = s.ImageMeta()
	}
	confirmed, err := s.Store.LicenseConfirmed(ctx, s.org(), b.ModelName, b.License.Version)
	if err != nil {
		return PreflightResult{}, err
	}
	return Preflight(b, plan, hardware, core, template, snapshot, available, confirmed), nil
}

// CreateInstall 创建幂等安装计划并启动串行执行。
func (s *Service) CreateInstall(ctx context.Context, id string, request CreateInstallRequest, operator string) (Install, error) {
	b, ok := s.Manifest.FindBundle(id)
	if !ok {
		return Install{}, &ValidationError{BundleID: id, Field: "id", Message: "套装不存在"}
	}
	if existing, found, err := s.Store.FindByRequestKey(ctx, request.RequestKey); err == nil && found {
		return existing, nil
	}
	plan, err := Expand(s.Manifest, id, request.Variant, request.SelectedOptions)
	if err != nil {
		return Install{}, err
	}
	confirmed, err := s.Store.LicenseConfirmed(ctx, s.org(), b.ModelName, b.License.Version)
	if err != nil {
		return Install{}, err
	}
	if !confirmed {
		return Install{}, ErrLicenseNotConfirmed
	}
	result, err := s.Preflight(ctx, id, request.Variant, request.SelectedOptions, operator)
	if err != nil {
		return Install{}, err
	}
	if !result.CanInstall {
		return Install{}, fmt.Errorf("%w：%s", ErrPreflightBlocked, firstBlockingReason(result.Checks))
	}
	repository := strings.TrimSpace(request.Repository)
	if repository == "" {
		repository = "models"
	}
	install := Install{
		ID:               fmt.Sprintf("bundle-%d", s.now().UnixNano()),
		BundleID:         b.ID,
		BundleVersion:    s.Manifest.BundleVersion,
		ManifestChecksum: s.Manifest.Checksum,
		Variant:          plan.Variant,
		SelectedOptions:  plan.SelectedOptions,
		TargetRepository: repository,
		Status:           StateQueued,
		TotalBytes:       plan.TotalBytes,
		RequestKey:       strings.TrimSpace(request.RequestKey),
		Operator:         operator,
	}
	if err := s.Store.CreateInstall(ctx, install, plan); err != nil {
		if errors.Is(err, ErrActiveInstall) {
			// 已有活动计划时返回它，保证重复提交幂等。
			if active, found, _ := s.Store.FindActive(ctx, b.ID, s.Manifest.BundleVersion, plan.Variant, repository); found {
				return active, nil
			}
		}
		return Install{}, err
	}
	if err := s.Installer.Start(ctx, install.ID); err != nil {
		return Install{}, err
	}
	return s.Store.GetInstall(ctx, install.ID)
}

// GetInstall 读取安装计划，并补充当前文件的实时下载字节。
// 只影响返回视图，不写库：CompletedBytes 落库值仍只统计已完成项。
func (s *Service) GetInstall(ctx context.Context, installID string) (Install, error) {
	install, err := s.Store.GetInstall(ctx, installID)
	if err != nil {
		return install, err
	}
	if s.Installer == nil {
		return install, nil
	}
	for index := range install.Items {
		item := &install.Items[index]
		if item.Status != ItemDownloading || item.ChildTaskID == "" {
			continue
		}
		state, err := s.Installer.Progress(ctx, item.ChildTaskID)
		if err != nil || state.DownloadedBytes <= 0 {
			continue
		}
		install.CurrentItemDownloaded = state.DownloadedBytes
		// 已下载但尚未完成的字节也计入进度，避免 20GB 单文件期间进度条停在 0%。
		if install.CompletedBytes+state.DownloadedBytes <= install.TotalBytes {
			install.CompletedBytes += state.DownloadedBytes
		}
	}
	return install, nil
}

// Action 执行安装计划操作。
func (s *Service) Action(ctx context.Context, installID, action string) error {
	switch action {
	case "pause":
		return s.Installer.Pause(ctx, installID)
	case "resume":
		return s.Installer.Start(ctx, installID)
	case "cancel":
		return s.Installer.Cancel(ctx, installID)
	case "retry":
		return s.Installer.RetryFailed(ctx, installID)
	default:
		return &ValidationError{Field: "action", Message: "不支持的操作：" + action}
	}
}

// ConfirmLicense 记录“模型 + 许可版本”的确认；同一版本重复提交保持幂等。
func (s *Service) ConfirmLicense(ctx context.Context, id, deliveryMethod, operator string) (LicenseConfirmation, error) {
	b, ok := s.Manifest.FindBundle(id)
	if !ok {
		return LicenseConfirmation{}, &ValidationError{BundleID: id, Field: "id", Message: "套装不存在"}
	}
	confirmation := LicenseConfirmation{
		ID:             fmt.Sprintf("lic-%d", s.now().UnixNano()),
		Org:            s.org(),
		ModelName:      b.ModelName,
		LicenseVersion: b.License.Version,
		LicenseName:    b.License.Name,
		OfficialURL:    b.License.OfficialURL,
		BundleID:       b.ID,
		BundleVersion:  s.Manifest.BundleVersion,
		DeliveryMethod: deliveryMethod,
		Operator:       operator,
		Environment:    s.environment(),
		ConfirmedAt:    s.now(),
	}
	if err := s.Store.ConfirmLicense(ctx, confirmation); err != nil {
		return LicenseConfirmation{}, err
	}
	// 幂等写入时保留首次记录：返回库里已有的那条，避免前端显示重复确认。
	history, _ := s.Store.ListLicenseConfirmations(ctx, s.org(), b.ModelName, 1)
	if len(history) > 0 {
		return history[0], nil
	}
	return confirmation, nil
}

// Link 把已齐备的文件关联到现有实例。
func (s *Service) Link(ctx context.Context, id, installID, instanceName, operator string) ([]string, error) {
	b, ok := s.Manifest.FindBundle(id)
	if !ok {
		return nil, &ValidationError{BundleID: id, Field: "id", Message: "套装不存在"}
	}
	var items []PlanItem
	if installID != "" {
		install, err := s.Store.GetInstall(ctx, installID)
		if err != nil {
			return nil, err
		}
		items = install.Plan.Items
	} else {
		plan, err := Expand(s.Manifest, id, "", nil)
		if err != nil {
			return nil, err
		}
		items = plan.Items
	}
	available := make([]PlanItem, 0, len(items))
	snapshot, _, _ := s.scan()
	for _, item := range items {
		if size, exists := snapshot.Sizes[item.TargetRelativePath]; exists && size == item.SizeBytes {
			available = append(available, item)
			continue
		}
		if item.Required {
			return nil, &ValidationError{BundleID: b.ID, Field: "items", Message: "必需文件未齐备：" + item.TargetRelativePath}
		}
	}
	if s.LinkModels == nil {
		return nil, &ValidationError{BundleID: b.ID, Field: "link", Message: "当前环境不支持实例模型关联"}
	}
	return s.LinkModels(ctx, instanceName, available)
}

// AddAcceptance 保存人工验收记录；结果只写人工事实。
func (s *Service) AddAcceptance(ctx context.Context, id string, acceptance Acceptance) (Acceptance, error) {
	b, ok := s.Manifest.FindBundle(id)
	if !ok {
		return Acceptance{}, &ValidationError{BundleID: id, Field: "id", Message: "套装不存在"}
	}
	if acceptance.Result != "pass" && acceptance.Result != "fail" && acceptance.Result != "partial" {
		return Acceptance{}, &ValidationError{BundleID: b.ID, Field: "result", Message: "只允许 pass／fail／partial"}
	}
	if strings.TrimSpace(acceptance.InstanceName) == "" {
		return Acceptance{}, &ValidationError{BundleID: b.ID, Field: "instanceName", Message: "必须记录验收实例"}
	}
	acceptance.ID = fmt.Sprintf("acc-%d", s.now().UnixNano())
	acceptance.BundleID = b.ID
	acceptance.BundleVersion = s.Manifest.BundleVersion
	acceptance.CreatedAt = s.now()
	if len(acceptance.Preset) == 0 {
		acceptance.Preset = []byte("{}")
	}
	if len(acceptance.Environment) == 0 {
		acceptance.Environment = []byte("{}")
	}
	if len(acceptance.Metrics) == 0 {
		acceptance.Metrics = []byte("{}")
	}
	if err := s.Store.CreateAcceptance(ctx, acceptance); err != nil {
		return Acceptance{}, err
	}
	return acceptance, nil
}

// ListAcceptances 分页返回验收历史。
func (s *Service) ListAcceptances(ctx context.Context, id string, limit, offset int) ([]Acceptance, int, error) {
	if _, ok := s.Manifest.FindBundle(id); !ok {
		return nil, 0, &ValidationError{BundleID: id, Field: "id", Message: "套装不存在"}
	}
	return s.Store.ListAcceptances(ctx, id, limit, offset)
}

func firstBlockingReason(checks []Check) string {
	for _, check := range checks {
		if check.Status == CheckBlock {
			if check.Detail != "" {
				return check.Title + "：" + check.Detail
			}
			return check.Title
		}
	}
	return "未知原因"
}
