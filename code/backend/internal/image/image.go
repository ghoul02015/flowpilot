// Package image 管理 ComfyUI 实例镜像的本地预置与后台拉取。
package image

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ghoul02015/flowpilot/internal/docker"
)

// ImageAPI 是镜像管理依赖的 Docker 操作子集，便于测试替换。
type ImageAPI interface {
	ImageExists(ctx context.Context, image string) (bool, error)
	PullImage(ctx context.Context, image string, onEvent func(docker.PullEvent)) error
}

// Status 是镜像拉取任务的对外视图。
type Status struct {
	Image     string    `json:"image"`
	State     string    `json:"state"` // idle | pulling | done | error
	Message   string    `json:"message"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ConflictError 表示已有拉取任务在进行中。
type ConflictError struct{ Image string }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("镜像 %s 正在拉取中，请勿重复提交", e.Image)
}

// ValidationError 表示请求参数不合法。
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

// Puller 串行管理镜像拉取任务；同一时刻只允许一个任务。
type Puller struct {
	docker ImageAPI
	mu     sync.Mutex
	status Status
}

// NewPuller 创建镜像拉取管理器。
func NewPuller(dockerClient ImageAPI) *Puller {
	return &Puller{docker: dockerClient, status: Status{State: "idle", UpdatedAt: time.Now()}}
}

// Exists 检查镜像是否在本地。
func (p *Puller) Exists(ctx context.Context, image string) (bool, error) {
	if err := validateImage(image); err != nil {
		return false, err
	}
	return p.docker.ImageExists(ctx, image)
}

// Status 返回当前拉取任务状态。
func (p *Puller) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

// Start 启动后台拉取；已有任务进行中时返回 ConflictError。
func (p *Puller) Start(image string) error {
	image = strings.TrimSpace(image)
	if err := validateImage(image); err != nil {
		return err
	}
	p.mu.Lock()
	if p.status.State == "pulling" {
		image := p.status.Image
		p.mu.Unlock()
		return &ConflictError{Image: image}
	}
	p.status = Status{Image: image, State: "pulling", Message: "正在连接镜像仓库", UpdatedAt: time.Now()}
	p.mu.Unlock()

	go func() {
		// 拉取可能持续数分钟，与请求生命周期解耦。
		err := p.docker.PullImage(context.Background(), image, func(event docker.PullEvent) {
			message := event.Status
			if event.Progress != "" {
				message = event.Status + " " + event.Progress
			}
			p.update("pulling", message)
		})
		if err != nil {
			p.update("error", err.Error())
			return
		}
		p.update("done", "镜像拉取完成")
	}()
	return nil
}

func (p *Puller) update(state, message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status.State = state
	p.status.Message = message
	p.status.UpdatedAt = time.Now()
}

// validateImage 校验镜像名格式，拒绝明显非法输入。
func validateImage(image string) error {
	if image == "" || len(image) > 255 || strings.ContainsAny(image, " \t\n\"'") {
		return &ValidationError{Message: "镜像名称不合法"}
	}
	return nil
}
