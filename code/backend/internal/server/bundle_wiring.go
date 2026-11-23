package server

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ghoul02015/flowpilot/internal/bundle"
	"github.com/ghoul02015/flowpilot/internal/database"
	"github.com/ghoul02015/flowpilot/internal/docker"
	"github.com/ghoul02015/flowpilot/internal/download"
	"github.com/ghoul02015/flowpilot/internal/hardware"
	"github.com/ghoul02015/flowpilot/internal/instance"
	"github.com/ghoul02015/flowpilot/internal/model"
)

// bundleDownloader 把下载引擎适配为套装编排所需的最小接口。
type bundleDownloader struct{ manager *download.Manager }

func (a bundleDownloader) Create(ctx context.Context, request bundle.DownloadRequest) (bundle.DownloadState, error) {
	task, err := a.manager.Create(download.Spec{
		URL:      request.URL,
		Category: request.Category,
		Name:     request.FileName,
		SHA256:   request.SHA256,
	})
	if err != nil {
		return bundle.DownloadState{}, err
	}
	return downloadStateOf(task), nil
}

func (a bundleDownloader) Get(_ context.Context, id string) (bundle.DownloadState, error) {
	task, err := a.manager.Get(id)
	if err != nil {
		return bundle.DownloadState{}, err
	}
	return downloadStateOf(task), nil
}

func (a bundleDownloader) Action(_ context.Context, id, action string) error {
	_, err := a.manager.Action(id, action)
	return err
}

func (a bundleDownloader) Delete(_ context.Context, id string) error {
	return a.manager.Delete(id)
}

// FindByTarget 查找同目录同名文件的已有任务，用于复用取消或重启后的残留分片。
func (a bundleDownloader) FindByTarget(_ context.Context, category, fileName string) (bundle.DownloadState, bool) {
	for _, task := range a.manager.List() {
		if task.Category == category && task.FileName == fileName {
			return downloadStateOf(task), true
		}
	}
	return bundle.DownloadState{}, false
}

func downloadStateOf(task download.Task) bundle.DownloadState {
	return bundle.DownloadState{
		ID: task.ID, Status: task.Status,
		DownloadedBytes: task.DownloadedBytes, TotalBytes: task.TotalBytes,
		Message: task.Message, SHA256: task.SHA256,
	}
}

// diskFreeBytes 返回目标路径所在文件系统的可用字节数。
func diskFreeBytes(path string) int64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0
	}
	return int64(stat.Bavail) * int64(stat.Bsize)
}

// NewBundleService 组装套装服务。清单校验失败时返回 nil，由 API 返回未启用。
func NewBundleService(
	dataStore *database.Store,
	modelManager *model.Manager,
	downloadManager *download.Manager,
	dockerClient *docker.Client,
	hardwareCollector *hardware.Collector,
	instanceManager *instance.Manager,
) *bundle.Service {
	if dataStore == nil || modelManager == nil || downloadManager == nil {
		return nil
	}
	manifest, err := bundle.Default()
	if err != nil {
		slog.Error("模型套装清单校验失败，套装功能停用", "error", err)
		return nil
	}
	store := bundle.NewStore(dataStore.DB())
	// 服务重启后把进行中的计划标为待继续，不伪造运行状态。
	if affected, err := store.MarkInterrupted(context.Background()); err != nil {
		slog.Warn("标记中断的套装安装失败", "error", err)
	} else if affected > 0 {
		slog.Info("套装安装计划已标记为待继续", "count", affected)
	}

	scan := func() (bundle.RepoSnapshot, int64, error) {
		report, err := modelManager.Scan()
		if err != nil {
			return bundle.RepoSnapshot{}, 0, err
		}
		sizes := make(map[string]int64, len(report.Files))
		for _, file := range report.Files {
			// 只把本地可写仓库计入安装目标；网络仓库不参与写入判定。
			if file.Origin != "" && file.Origin != "local" {
				continue
			}
			sizes[file.Path] = file.SizeBytes
		}
		return bundle.NewRepoSnapshot(report.Root, sizes), diskFreeBytes(modelManager.Root()), nil
	}

	var hardwareCache struct {
		sync.Mutex
		summary bundle.HardwareSummary
		at      time.Time
	}
	hardwareFn := func() bundle.HardwareSummary {
		hardwareCache.Lock()
		defer hardwareCache.Unlock()
		if time.Since(hardwareCache.at) < 10*time.Second {
			return hardwareCache.summary
		}
		summary := bundle.HardwareSummary{CollectedAt: time.Now().UTC()}
		if hardwareCollector != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			report := hardwareCollector.Collect(ctx)
			summary.HostModel = report.MachineModel
			summary.UnifiedMemory = report.GPUMemoryMode == "unified"
			if report.GPUError != "" {
				summary.Error = report.GPUError
			}
			for _, gpu := range report.GPUs {
				memory := int64(0)
				if gpu.MemoryTotalMB != nil {
					memory = int64(*gpu.MemoryTotalMB)
				}
				summary.GPUs = append(summary.GPUs, bundle.GPUInfo{Index: gpu.Index, Name: gpu.Name, MemoryTotalMB: memory})
			}
		}
		hardwareCache.summary = summary
		hardwareCache.at = time.Now()
		return summary
	}

	imageMeta := func() (string, string) {
		if dockerClient == nil {
			return "", ""
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		images, err := dockerClient.Images(ctx)
		if err != nil {
			return "", ""
		}
		for _, img := range images {
			if img.Labels["flowpilot.kind"] != "comfyui" {
				continue
			}
			version := img.Labels["org.opencontainers.image.version"]
			if version == "" {
				version = img.Labels["flowpilot.comfyui.version"]
			}
			if version == "" {
				version = img.Labels["flowpilot.comfyui.ref"]
			}
			if version != "" {
				// 模板包版本目前未写入镜像标签，交由预检显示待确认。
				return version, ""
			}
		}
		return "", ""
	}

	linkModels := func(ctx context.Context, instanceName string, items []bundle.PlanItem) ([]string, error) {
		if instanceManager == nil {
			return nil, &bundle.ValidationError{Field: "instance", Message: "实例管理未启用"}
		}
		supported, existing, err := instanceManager.ListModelLinks(ctx, instanceName)
		if err != nil {
			return nil, err
		}
		if !supported {
			return nil, &bundle.ValidationError{Field: "instance", Message: "该实例由旧版本创建，缺少模型关联挂载，需重建后使用"}
		}
		known := map[string]bool{}
		links := make([]instance.ModelLink, 0, len(existing)+len(items))
		for _, link := range existing {
			key := link.Category + "/" + link.Name
			if known[key] {
				continue
			}
			known[key] = true
			links = append(links, link)
		}
		added := []string{}
		for _, item := range items {
			name := item.TargetRelativePath
			if index := strings.LastIndex(name, "/"); index >= 0 {
				name = name[index+1:]
			}
			key := item.Category + "/" + name
			if known[key] {
				continue
			}
			known[key] = true
			links = append(links, instance.ModelLink{Category: item.Category, Name: name})
			added = append(added, key)
		}
		if _, _, err := instanceManager.SetModelLinksFor(ctx, instanceName, links); err != nil {
			return nil, err
		}
		return added, nil
	}

	installer := bundle.NewInstaller(store, manifest, bundleDownloader{manager: downloadManager}, scan)
	return &bundle.Service{
		Manifest:   manifest,
		Store:      store,
		Installer:  installer,
		ScanRepo:   scan,
		Hardware:   hardwareFn,
		ImageMeta:  imageMeta,
		LinkModels: linkModels,
		Org:        "default",
		Environment: func() string {
			if host, err := os.Hostname(); err == nil && strings.TrimSpace(host) != "" {
				return host
			}
			return "default"
		}(),
	}
}
