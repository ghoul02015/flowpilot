package bundle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// DownloadRequest 是套装安装向下载引擎提交的单文件请求。
type DownloadRequest struct {
	URL      string
	Category string
	FileName string
	SHA256   string
}

// DownloadState 是下载引擎返回的任务状态。
type DownloadState struct {
	ID              string
	Status          string // running | paused | completed | failed | cancelled
	DownloadedBytes int64
	TotalBytes      int64
	Message         string
}

// Downloader 是安装编排依赖的最小下载能力集合。
type Downloader interface {
	Create(ctx context.Context, request DownloadRequest) (DownloadState, error)
	Get(ctx context.Context, id string) (DownloadState, error)
	Action(ctx context.Context, id, action string) error
	// FindByTarget 按目录与文件名查找已有任务，用于复用取消或重启后残留的分片。
	FindByTarget(ctx context.Context, category, fileName string) (DownloadState, bool)
}

// DefaultMirrorBase 是国内可达的模型传输通道；内容一致性由官方 SHA256 校验保证。
const DefaultMirrorBase = "https://hf-mirror.com"

// Installer 串行执行套装安装：同一时刻只处理一个文件。
type Installer struct {
	store      *Store
	manifest   *Manifest
	downloader Downloader
	mirrorBase string
	// scan 返回目标仓库快照与可用空间（字节）。
	scan      func() (RepoSnapshot, int64, error)
	pollEvery time.Duration
	now       func() time.Time

	mu        sync.Mutex
	workerGen map[string]int64 // installID -> 当前 worker 代次
	genSeq    int64

	workers map[string]context.CancelFunc
	current map[string]string // installID -> 当前子任务 ID
}

// NewInstaller 创建安装编排器。
func NewInstaller(store *Store, manifest *Manifest, downloader Downloader, scan func() (RepoSnapshot, int64, error)) *Installer {
	return &Installer{
		store:      store,
		manifest:   manifest,
		downloader: downloader,
		mirrorBase: DefaultMirrorBase,
		scan:       scan,
		pollEvery:  time.Second,
		now:        func() time.Time { return time.Now().UTC() },
		workers:    map[string]context.CancelFunc{},
		workerGen:  map[string]int64{},
		current:    map[string]string{},
	}
}

// SetMirrorBase 覆盖下载通道（测试或内网镜像使用）。
func (in *Installer) SetMirrorBase(base string) {
	if strings.TrimSpace(base) != "" {
		in.mirrorBase = strings.TrimRight(base, "/")
	}
}

// SetPollInterval 覆盖轮询间隔。
func (in *Installer) SetPollInterval(interval time.Duration) {
	if interval > 0 {
		in.pollEvery = interval
	}
}

// Start 启动（或恢复）一个安装计划的串行执行。
func (in *Installer) Start(ctx context.Context, installID string) error {
	install, err := in.store.GetInstall(ctx, installID)
	if err != nil {
		return err
	}
	switch install.Status {
	case StateQueued, StateInstalling, StatePaused:
	default:
		return &ValidationError{BundleID: install.BundleID, Field: "status", Message: "计划已结束，不能重新启动"}
	}
	in.mu.Lock()
	if _, exists := in.workers[installID]; exists {
		in.mu.Unlock()
		return nil
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	in.genSeq++
	generation := in.genSeq
	in.workers[installID] = cancel
	in.workerGen[installID] = generation
	in.mu.Unlock()

	go in.run(workerCtx, installID, install, generation)
	return nil
}

// Pause 暂停计划：停止轮询并暂停当前子任务。
func (in *Installer) Pause(ctx context.Context, installID string) error {
	install, err := in.store.GetInstall(ctx, installID)
	if err != nil {
		return err
	}
	if install.Status != StateInstalling && install.Status != StateQueued {
		return &ValidationError{BundleID: install.BundleID, Field: "status", Message: "只有进行中的计划可以暂停"}
	}
	in.stopWorker(installID)
	if taskID := in.currentTask(installID); taskID != "" {
		if err := in.downloader.Action(ctx, taskID, "pause"); err != nil {
			slog.Warn("暂停子下载任务失败", "install", installID, "task", taskID, "error", err)
		}
	}
	return in.store.UpdateInstall(ctx, installID, StatePaused, install.CurrentItemKey, install.CompletedBytes, "", "")
}

// Cancel 取消计划：已完成文件保留，不自动删除。
func (in *Installer) Cancel(ctx context.Context, installID string) error {
	install, err := in.store.GetInstall(ctx, installID)
	if err != nil {
		return err
	}
	switch install.Status {
	case StateInstalling, StateQueued, StatePaused, StateFailed:
	default:
		return &ValidationError{BundleID: install.BundleID, Field: "status", Message: "计划已结束，无需取消"}
	}
	in.stopWorker(installID)
	if taskID := in.currentTask(installID); taskID != "" {
		if err := in.downloader.Action(ctx, taskID, "cancel"); err != nil {
			slog.Warn("取消子下载任务失败", "install", installID, "task", taskID, "error", err)
		}
	}
	for _, item := range install.Items {
		if item.Status == ItemDownloading || item.Status == ItemVerifying {
			_ = in.store.UpdateItem(ctx, installID, item.ItemKey, ItemPaused, item.ChildTaskID, item.ActualBytes, item.ActualSHA256, "", "", false)
		}
	}
	return in.store.UpdateInstall(ctx, installID, StateCancelled, "", install.CompletedBytes, "", "已取消，已完成文件保留")
}

// RetryFailed 只重置失败项并重新启动，已完成项不重复下载。
func (in *Installer) RetryFailed(ctx context.Context, installID string) error {
	install, err := in.store.GetInstall(ctx, installID)
	if err != nil {
		return err
	}
	if install.Status != StateFailed && install.Status != StateCancelled && install.Status != StatePaused {
		return &ValidationError{BundleID: install.BundleID, Field: "status", Message: "只有失败、取消或暂停的计划可以重试"}
	}
	reset := 0
	for _, item := range install.Items {
		if item.Status == ItemPresent {
			continue
		}
		reset++
		if err := in.store.UpdateItem(ctx, installID, item.ItemKey, ItemMissing, "", 0, "", "", "", true); err != nil {
			return err
		}
	}
	if reset == 0 {
		return &ValidationError{BundleID: install.BundleID, Field: "items", Message: "没有需要重试的文件"}
	}
	if err := in.store.UpdateInstall(ctx, installID, StateInstalling, "", install.CompletedBytes, "", ""); err != nil {
		return err
	}
	return in.Start(ctx, installID)
}

// Progress 返回子任务当前进度，供 API 补实时字节（不落库）。
func (in *Installer) Progress(ctx context.Context, taskID string) (DownloadState, error) {
	if taskID == "" {
		return DownloadState{}, nil
	}
	return in.downloader.Get(ctx, taskID)
}

// Wait 等待安装结束，仅供测试与同步调用场景使用。
func (in *Installer) Wait(installID string) {
	for {
		in.mu.Lock()
		_, running := in.workers[installID]
		in.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(in.pollEvery)
	}
}

func (in *Installer) stopWorker(installID string) {
	in.mu.Lock()
	cancel, ok := in.workers[installID]
	delete(in.workers, installID)
	delete(in.workerGen, installID)
	delete(in.current, installID)
	in.mu.Unlock()
	if ok {
		cancel()
	}
}

func (in *Installer) currentTask(installID string) string {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.current[installID]
}

func (in *Installer) setCurrentTask(installID, taskID string, generation int64) {
	in.mu.Lock()
	defer in.mu.Unlock()
	// 只有当前代次的 worker 才能写，避免旧 worker 覆盖新 worker。
	if in.workerGen[installID] != generation {
		return
	}
	if taskID == "" {
		delete(in.current, installID)
		return
	}
	in.current[installID] = taskID
}

func (in *Installer) finishWorker(installID string, generation int64) {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.workerGen[installID] != generation {
		return
	}
	delete(in.workers, installID)
	delete(in.workerGen, installID)
	delete(in.current, installID)
}

// run 是串行安装主循环。
func (in *Installer) run(ctx context.Context, installID string, install Install, generation int64) {
	defer in.finishWorker(installID, generation)

	if install.Status == StateQueued || install.Status == StatePaused {
		if err := in.store.UpdateInstall(ctx, installID, StateInstalling, install.CurrentItemKey, install.CompletedBytes, "", ""); err != nil {
			slog.Error("更新安装状态失败", "install", installID, "error", err)
			return
		}
	}

	snapshot, _, err := in.scan()
	if err != nil {
		_ = in.store.UpdateInstall(ctx, installID, StateFailed, "", install.CompletedBytes, "scan_failed", "扫描目标仓库失败："+err.Error())
		return
	}

	var completed int64
	for _, item := range install.Items {
		if ctx.Err() != nil {
			return
		}
		if item.Status == ItemPresent {
			completed += item.ExpectedBytes
			continue
		}
		// 已存在且大小一致的文件直接复用，避免重复下载。
		if size, exists := snapshot.Sizes[item.TargetPath]; exists && size == item.ExpectedBytes {
			_ = in.store.UpdateItem(ctx, installID, item.ItemKey, ItemPresent, "", size, item.ExpectedSHA256, "", "", false)
			completed += item.ExpectedBytes
			continue
		}
		if err := in.installItem(ctx, installID, install, item, &completed, generation); err != nil {
			if ctx.Err() != nil {
				return
			}
			if item.Required {
				_ = in.store.UpdateInstall(ctx, installID, StateFailed, item.ItemKey, completed, "item_failed", err.Error())
				return
			}
			// 可选项失败不阻断必需项。
			slog.Warn("可选文件安装失败，继续后续项", "install", installID, "item", item.ItemKey, "error", err)
		}
	}
	_ = in.store.UpdateInstall(ctx, installID, StateFilesReady, "", completed, "", "")
}

func (in *Installer) installItem(ctx context.Context, installID string, install Install, item InstallItem, completed *int64, generation int64) error {
	plan := install.Plan
	var planItem *PlanItem
	for index := range plan.Items {
		if plan.Items[index].Key == item.ItemKey {
			planItem = &plan.Items[index]
			break
		}
	}
	if planItem == nil {
		return fmt.Errorf("安装快照缺少文件项 %s", item.ItemKey)
	}

	_ = in.store.UpdateInstall(ctx, installID, StateInstalling, item.ItemKey, *completed, "", "")
	_ = in.store.UpdateItem(ctx, installID, item.ItemKey, ItemDownloading, item.ChildTaskID, 0, "", "", "", false)

	taskID := item.ChildTaskID
	// 恢复已有子任务；缺失或已结束时重新创建。
	if taskID != "" {
		state, err := in.downloader.Get(ctx, taskID)
		if err != nil || state.Status == "failed" || state.Status == "cancelled" {
			taskID = ""
		} else if state.Status == "paused" {
			if err := in.downloader.Action(ctx, taskID, "resume"); err != nil {
				taskID = ""
			}
		}
	}
	if taskID == "" {
		request := DownloadRequest{
			URL:      in.buildURL(*planItem),
			Category: planItem.Category,
			FileName: fileNameOf(planItem.TargetRelativePath),
			SHA256:   planItem.SHA256,
		}
		state, err := in.downloader.Create(ctx, request)
		if err != nil {
			// 取消或平台重启后，同目标可能留有 paused/failed 任务；
			// 直接复用可保留已下载分片，否则新任务会被"目标已有任务在进行中"拒绝。
			if existing, found := in.downloader.FindByTarget(ctx, request.Category, request.FileName); found {
				switch existing.Status {
				case "running", "paused":
					if existing.Status == "paused" {
						if resumeErr := in.downloader.Action(ctx, existing.ID, "resume"); resumeErr != nil {
							break
						}
					}
					state, err = existing, nil
				case "failed", "cancelled":
					if retryErr := in.downloader.Action(ctx, existing.ID, "retry"); retryErr == nil {
						state, err = existing, nil
					}
				case "completed":
					_ = in.store.UpdateItem(ctx, installID, item.ItemKey, ItemPresent, existing.ID, item.ExpectedBytes, item.ExpectedSHA256, "", "", false)
					*completed += item.ExpectedBytes
					_ = in.store.UpdateInstall(ctx, installID, StateInstalling, item.ItemKey, *completed, "", "")
					return nil
				}
			}
		}
		if err != nil {
			_ = in.store.UpdateItem(ctx, installID, item.ItemKey, ItemMissing, "", 0, "", "create_failed", err.Error(), true)
			return fmt.Errorf("创建下载任务失败：%w", err)
		}
		taskID = state.ID
		_ = in.store.UpdateItem(ctx, installID, item.ItemKey, ItemDownloading, taskID, 0, "", "", "", true)
	}
	in.setCurrentTask(installID, taskID, generation)
	defer in.setCurrentTask(installID, "", generation)

	ticker := time.NewTicker(in.pollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			state, err := in.downloader.Get(ctx, taskID)
			if err != nil {
				return fmt.Errorf("读取下载状态失败：%w", err)
			}
			switch state.Status {
			case "completed":
				_ = in.store.UpdateItem(ctx, installID, item.ItemKey, ItemPresent, taskID, item.ExpectedBytes, item.ExpectedSHA256, "", "", false)
				*completed += item.ExpectedBytes
				_ = in.store.UpdateInstall(ctx, installID, StateInstalling, item.ItemKey, *completed, "", "")
				return nil
			case "failed":
				message := state.Message
				if message == "" {
					message = "下载失败"
				}
				_ = in.store.UpdateItem(ctx, installID, item.ItemKey, ItemMissing, taskID, state.DownloadedBytes, "", "download_failed", message, true)
				return fmt.Errorf("%s", message)
			case "cancelled":
				_ = in.store.UpdateItem(ctx, installID, item.ItemKey, ItemPaused, taskID, state.DownloadedBytes, "", "cancelled", "已取消", false)
				return errors.New("下载已取消")
			}
		}
	}
}

func (in *Installer) buildURL(item PlanItem) string {
	return fmt.Sprintf("%s/%s/resolve/%s/%s", in.mirrorBase, item.Repo, item.Revision, item.FilePath)
}

func fileNameOf(relativePath string) string {
	if index := strings.LastIndex(relativePath, "/"); index >= 0 {
		return relativePath[index+1:]
	}
	return relativePath
}
