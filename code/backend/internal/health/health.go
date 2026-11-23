// Package health 应用层健康检查器：独立于页面轮询周期探测实例 ComfyUI
// 可达性，推导应用状态（ready/starting/degraded/failed/not_running），
// 状态迁移写入实例事件（含恢复事件）。
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ghoul02015/flowpilot/internal/database"
	"github.com/ghoul02015/flowpilot/internal/instance"
)

// 应用状态常量。
const (
	StateReady      = "ready"
	StateStarting   = "starting"
	StateDegraded   = "degraded"
	StateFailed     = "failed"
	StateNotRunning = "not_running"
	StateUnknown    = "unknown"
)

// 检查节奏：启动期短间隔、稳定期长间隔、失败指数退避。
const (
	startingInterval = 3 * time.Second
	stableInterval   = 10 * time.Second
	maxBackoff       = 60 * time.Second
	probeTimeout     = 3 * time.Second
	// 连续失败阈值：达到即 failed。
	failedThreshold = 3
	// 有界并发。
	maxConcurrency = 8
)

// InstanceHealth 三层状态视图。
type InstanceHealth struct {
	Name        string `json:"name"`
	UUID        string `json:"uuid,omitempty"`
	Container   string `json:"container"` // running/stopped/error/unknown
	App         string `json:"app"`       // ready/starting/degraded/failed/not_running
	LastCheckAt string `json:"lastCheckAt,omitempty"`
	LatenciesMS []int  `json:"latenciesMs"` // 最近 3 次探测延迟
	NextCheckAt string `json:"nextCheckAt,omitempty"`
}

// trackState 检查器内部状态。
type trackState struct {
	app          string
	failures     int
	lastCheck    time.Time
	nextCheck    time.Time
	firstRunning time.Time // starting 计时起点
	latencies    []int
}

// Checker 健康检查器。
type Checker struct {
	mu           sync.Mutex
	states       map[string]*trackState // key: 实例名
	lister       func(context.Context) ([]instance.Instance, error)
	gateway      string
	client       *http.Client
	store        *database.Store
	startTimeout time.Duration
}

// NewChecker 创建检查器；lister 提供实例清单，store 可为 nil（事件静默丢弃）。
func NewChecker(lister func(context.Context) ([]instance.Instance, error), gateway string, store *database.Store, startTimeout time.Duration) *Checker {
	if startTimeout <= 0 {
		startTimeout = 180 * time.Second
	}
	return &Checker{
		states:       map[string]*trackState{},
		lister:       lister,
		gateway:      gateway,
		client:       &http.Client{Timeout: probeTimeout},
		store:        store,
		startTimeout: startTimeout,
	}
}

// Run 阻塞运行检查循环（由 goroutine 启动）。
func (c *Checker) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.pass(ctx)
			ticker.Reset(c.nextTickIn())
		}
	}
}

// nextTickIn 计算下一轮检查间隔：任一实例处于 starting 用短间隔，否则长间隔。
func (c *Checker) nextTickIn() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, state := range c.states {
		if state.app == StateStarting {
			return startingInterval
		}
	}
	return stableInterval
}

// pass 执行一轮检查：刷新实例清单，按到期逐个探测（有界并发）。
func (c *Checker) pass(ctx context.Context) {
	instances, err := c.lister(ctx)
	if err != nil {
		return
	}
	live := map[string]instance.Instance{}
	for _, item := range instances {
		live[item.Name] = item
	}
	c.mu.Lock()
	// 清理已删除实例的状态。
	for name := range c.states {
		if _, ok := live[name]; !ok {
			delete(c.states, name)
		}
	}
	due := make([]instance.Instance, 0)
	now := time.Now()
	for name, item := range live {
		state, ok := c.states[name]
		if !ok {
			// 新发现实例：初始化状态。
			if item.Status == "running" {
				state = &trackState{app: StateStarting, firstRunning: now, nextCheck: now}
			} else {
				state = &trackState{app: StateNotRunning, nextCheck: now}
			}
			c.states[name] = state
		}
		if now.After(state.nextCheck) || now.Equal(state.nextCheck) {
			due = append(due, item)
		}
	}
	c.mu.Unlock()

	semaphore := make(chan struct{}, maxConcurrency)
	var wait sync.WaitGroup
	for _, item := range due {
		wait.Add(1)
		go func(target instance.Instance) {
			defer wait.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			c.probe(target)
		}(item)
	}
	wait.Wait()
}

// probe 探测单个实例并应用状态机。
func (c *Checker) probe(target instance.Instance) {
	started := time.Now()
	var latency int
	reachable := false
	detail := ""
	if target.Status != "running" {
		// 容器非运行：直接迁移 not_running。
		c.transition(target, func(state *trackState) (string, string, bool) {
			if state.app != StateNotRunning {
				return StateNotRunning, "容器未运行", true
			}
			return StateNotRunning, "", false
		})
		return
	}
	base := "http://" + c.gateway + ":" + strconv.Itoa(target.Port)
	response, err := c.client.Get(base + "/system_stats")
	if err != nil {
		detail = err.Error()
	} else {
		defer response.Body.Close()
		var stats struct {
			System struct {
				ComfyuiVersion string `json:"comfyui_version"`
			} `json:"system"`
		}
		if err := json.NewDecoder(response.Body).Decode(&stats); err != nil {
			detail = "响应解析失败：" + err.Error()
		} else {
			reachable = true
			latency = int(time.Since(started).Milliseconds())
			detail = "ComfyUI " + stats.System.ComfyuiVersion
		}
	}
	c.mu.Lock()
	state := c.states[target.Name]
	if state == nil {
		state = &trackState{app: StateStarting, firstRunning: started}
		c.states[target.Name] = state
	}
	state.lastCheck = time.Now()
	state.failures++
	if reachable {
		state.failures = 0
		state.latencies = append(state.latencies, latency)
		if len(state.latencies) > 3 {
			state.latencies = state.latencies[len(state.latencies)-3:]
		}
	}
	backoff := time.Duration(0)
	if !reachable {
		backoff = backoffFor(state.failures)
	}
	state.nextCheck = time.Now().Add(backoff)
	c.mu.Unlock()

	if reachable {
		c.transition(target, func(state *trackState) (string, string, bool) {
			switch state.app {
			case StateStarting:
				return StateReady, detail, true
			case StateDegraded, StateFailed:
				return StateReady, "探测恢复：" + detail, true
			default:
				return StateReady, "", false
			}
		})
	} else {
		c.transition(target, func(state *trackState) (string, string, bool) {
			switch state.app {
			case StateStarting:
				if time.Since(state.firstRunning) > c.startTimeout {
					return StateFailed, "启动超时（" + c.startTimeout.String() + "）：" + detail, true
				}
				return StateStarting, "", false
			case StateReady:
				return StateDegraded, "探测失败：" + detail, true
			case StateDegraded:
				if state.failures >= failedThreshold {
					return StateFailed, "连续 " + strconv.Itoa(state.failures) + " 次失败：" + detail, true
				}
				return StateDegraded, "", false
			default:
				return state.app, "", false
			}
		})
	}
}

// transition 在锁内应用状态机结果并写事件。
// kind 由 prev→next 迁移决定：starting→ready 记 ready；degraded/failed→ready
// 记 recovered；其余记 next 本身（degraded/failed/stopped）。
func (c *Checker) transition(target instance.Instance, mover func(*trackState) (next, detail string, changed bool)) {
	c.mu.Lock()
	state := c.states[target.Name]
	if state == nil {
		c.mu.Unlock()
		return
	}
	prev := state.app
	next, detail, changed := mover(state)
	state.app = next
	c.mu.Unlock()
	if !changed {
		return
	}
	kind := next
	switch {
	case next == StateReady && (prev == StateDegraded || prev == StateFailed):
		kind = "recovered"
	case next == StateReady:
		kind = "ready"
	case next == StateNotRunning:
		kind = "stopped"
	}
	if c.store != nil {
		c.store.RecordEvent(target.UUID, target.Name, kind, detail, 0)
	}
	slog.Info("实例健康事件", "name", target.Name, "kind", kind, "detail", detail)
}

// HealthOf 返回实例三层状态（供 API 调用；未跟踪实例返回 unknown/starting）。
func (c *Checker) HealthOf(target instance.Instance) InstanceHealth {
	health := InstanceHealth{Name: target.Name, UUID: target.UUID, Container: target.Status, App: StateUnknown, LatenciesMS: []int{}}
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.states[target.Name]
	if !ok {
		if target.Status == "running" {
			health.App = StateStarting
		} else {
			health.App = StateNotRunning
		}
		return health
	}
	health.App = state.app
	if !state.lastCheck.IsZero() {
		health.LastCheckAt = state.lastCheck.UTC().Format(time.RFC3339)
	}
	health.LatenciesMS = append(health.LatenciesMS, state.latencies...)
	if !state.nextCheck.IsZero() {
		health.NextCheckAt = state.nextCheck.UTC().Format(time.RFC3339)
	}
	return health
}

// backoffFor 失败次数→退避间隔（指数，上限 60s）。
func backoffFor(failures int) time.Duration {
	interval := stableInterval
	for i := 1; i < failures && interval < maxBackoff; i++ {
		interval *= 2
	}
	if interval > maxBackoff {
		interval = maxBackoff
	}
	return interval
}

// MarkStarted 操作钩子：实例启动后重置为 starting 并立即检查。
func (c *Checker) MarkStarted(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.states[name] = &trackState{app: StateStarting, firstRunning: time.Now(), nextCheck: time.Now()}
}

// MarkStopped 操作钩子：实例停止。
func (c *Checker) MarkStopped(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if state, ok := c.states[name]; ok {
		state.app = StateNotRunning
		state.failures = 0
	}
}

var _ = fmt.Sprintf // 保持 fmt 引用（错误信息构造）
