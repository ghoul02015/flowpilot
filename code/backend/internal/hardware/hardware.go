// Package hardware 采集宿主机硬件与 GPU 实时指标。
//
// 设计目标：容器内仅依赖 Linux 标准接口（/proc、statfs）和宿主机
// nvidia-smi 二进制，不引入第三方库；任何单项采集失败都不影响其他指标，
// GPU 不可用时返回明确的降级信息。
package hardware

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// GPU 表示一块 GPU 的实时指标；统一内存架构下显存字段为 nil。
type GPU struct {
	Index              int      `json:"index"`
	Name               string   `json:"name"`
	MemoryTotalMB      *float64 `json:"memoryTotalMB"`
	MemoryUsedMB       *float64 `json:"memoryUsedMB"`
	UtilizationPercent *float64 `json:"utilizationPercent"`
	TemperatureC       *float64 `json:"temperatureC"`
	PowerDrawW         *float64 `json:"powerDrawW"`
	PowerLimitW        *float64 `json:"powerLimitW"`
}

// Report 是一次完整硬件采集的结果。
type Report struct {
	CollectedAt       time.Time `json:"collectedAt"`
	HostName          string    `json:"hostName"`
	MachineModel      string    `json:"machineModel,omitempty"`
	OS                string    `json:"os"`
	Architecture      string    `json:"architecture"`
	CPUModel          string    `json:"cpuModel"`
	CPUCores          int       `json:"cpuCores"`
	CPUUsagePercent   *float64  `json:"cpuUsagePercent"`
	MemoryTotalMB     uint64    `json:"memoryTotalMB"`
	MemoryUsedMB      uint64    `json:"memoryUsedMB"`
	StoragePath       string    `json:"storagePath"`
	StorageTotalBytes uint64    `json:"storageTotalBytes"`
	StorageUsedBytes  uint64    `json:"storageUsedBytes"`
	GPUAvailable      bool      `json:"gpuAvailable"`
	GPUError          string    `json:"gpuError,omitempty"`
	GPUMemoryMode     string    `json:"gpuMemoryMode"` // discrete | unified | none
	// DriverVersion / CUDAVersion 来自 nvidia-smi 头部，解析失败或不可用时为空。
	DriverVersion string `json:"driverVersion,omitempty"`
	CUDAVersion   string `json:"cudaVersion,omitempty"`
	GPUs          []GPU  `json:"gpus"`
}

// Config 控制采集器的数据来源，便于容器挂载和测试替换。
type Config struct {
	// ProcRoot 是 /proc 的挂载位置，默认 /proc。
	ProcRoot string
	// HostRoot 是宿主机只读文件（hostname、os-release）的挂载前缀，默认 /host。
	HostRoot string
	// DataDir 用于统计数据盘用量，默认 /data/flowpilot。
	DataDir string
	// NvidiaSMIBin 是 nvidia-smi 路径，默认从 PATH 查找；置空字符串可禁用 GPU 采集。
	NvidiaSMIBin string
}

// Collector 线程安全；CPU 利用率基于两次采样的差值计算。
type Collector struct {
	procRoot     string
	hostRoot     string
	dataDir      string
	nvidiaSMIBin string

	// queryGPU 可替换用于测试；默认执行 nvidia-smi。
	queryGPU func(ctx context.Context) (string, error)
	// queryVersions 可替换用于测试；默认执行 nvidia-smi 读取驱动/CUDA 版本。
	queryVersions func(ctx context.Context) (string, error)

	mu        sync.Mutex
	prevIdle  uint64
	prevTotal uint64
	prevValid bool

	// 短时缓存与单飞：并发请求共享同一次采集，避免重复执行 nvidia-smi。
	cache      *Report
	cacheAt    time.Time
	last       Report
	hasLast    bool
	collecting bool
	wait       chan struct{}
	// nvidia-smi 不存在等持续性错误使用负缓存，避免每次刷新都重复失败。
	gpuNegUntil time.Time
	gpuNegErr   string
	collects    int
	cacheTTL    time.Duration
}

// NewCollector 按配置创建采集器；零值字段取默认值。
func NewCollector(config Config) *Collector {
	collector := &Collector{
		procRoot:     orDefault(config.ProcRoot, "/proc"),
		hostRoot:     orDefault(config.HostRoot, "/host"),
		dataDir:      orDefault(config.DataDir, "/data/flowpilot"),
		nvidiaSMIBin: orDefault(config.NvidiaSMIBin, "nvidia-smi"),
		// 生产默认开启 2 秒合并缓存；此前漏设导致并发请求退化为每次全量采集。
		cacheTTL: collectCacheTTL,
	}
	collector.queryGPU = collector.execNvidiaSMI
	collector.queryVersions = collector.execNvidiaSMIHeader
	return collector
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// 采集缓存与负缓存参数；测试可调整。
const (
	collectCacheTTL = 2 * time.Second
	gpuNegativeTTL  = 60 * time.Second
)

// Collect 执行一次完整采集；短时并发请求合并为一次执行。
func (c *Collector) Collect(ctx context.Context) Report {
	c.mu.Lock()
	if c.cacheTTL > 0 && c.cache != nil && time.Since(c.cacheAt) < c.cacheTTL {
		report := *c.cache
		c.mu.Unlock()
		return report
	}
	if c.collecting {
		wait := c.wait
		c.mu.Unlock()
		<-wait
		c.mu.Lock()
		// 等待者必须拿到刚完成的报告：cacheTTL 为 0 时 cache 不会写入，
		// 早期实现会返回零值报告，界面因此出现“未检测到 GPU”的假告警。
		report, ok := c.last, c.hasLast
		c.mu.Unlock()
		if ok {
			return report
		}
		// 极端情况下（采集 goroutine 异常退出）自行补采一次，保证有真实数据。
		return c.Collect(ctx)
	}
	c.collecting = true
	wait := make(chan struct{})
	c.wait = wait
	c.mu.Unlock()

	report := c.collectOnce(ctx)

	c.mu.Lock()
	c.last = report
	c.hasLast = true
	if c.cacheTTL > 0 {
		c.cache = &report
		c.cacheAt = time.Now()
	}
	c.collecting = false
	c.wait = nil
	c.collects++
	close(wait)
	c.mu.Unlock()
	return report
}

// SetCacheTTL 覆盖短时缓存有效期；测试使用，ttl<=0 表示不缓存。
func (c *Collector) SetCacheTTL(ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cacheTTL = ttl
	c.cache = nil
	c.cacheAt = time.Time{}
	c.last = Report{}
	c.hasLast = false
}

// collectOnce 是实际执行采集的路径。
func (c *Collector) collectOnce(ctx context.Context) Report {
	report := Report{
		CollectedAt:   time.Now().UTC(),
		HostName:      c.hostName(),
		MachineModel:  c.machineModel(),
		OS:            c.osName(),
		Architecture:  normalizeArch(runtime.GOARCH),
		CPUModel:      c.cpuModel(),
		GPUMemoryMode: "none",
		GPUs:          []GPU{},
	}
	report.CPUCores = c.cpuCores()
	report.CPUUsagePercent = c.cpuUsage()
	report.MemoryTotalMB, report.MemoryUsedMB = c.memory()
	report.StoragePath, report.StorageTotalBytes, report.StorageUsedBytes = c.storage()

	c.mu.Lock()
	negative := time.Now().Before(c.gpuNegUntil)
	negativeErr := c.gpuNegErr
	c.mu.Unlock()
	if negative {
		report.GPUAvailable = false
		report.GPUError = negativeErr
		return report
	}

	report.GPUs, report.GPUAvailable, report.GPUMemoryMode, report.GPUError = c.gpus(ctx)
	if !report.GPUAvailable && persistentGPUError(report.GPUError) {
		c.mu.Lock()
		c.gpuNegUntil = time.Now().Add(gpuNegativeTTL)
		c.gpuNegErr = report.GPUError
		c.mu.Unlock()
	}
	if report.GPUAvailable {
		report.DriverVersion, report.CUDAVersion = c.versions(ctx)
	}
	return report
}

// persistentGPUError 判断是否为“命令不存在”这类持续错误。
func persistentGPUError(message string) bool {
	lower := strings.ToLower(message)
	for _, marker := range []string{"executable file not found", "no such file", "not found", "命令不存在"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func (c *Collector) hostName() string {
	for _, candidate := range []string{
		filepath.Join(c.hostRoot, "etc", "hostname"),
		filepath.Join(c.procRoot, "sys", "kernel", "hostname"),
	} {
		if content, err := os.ReadFile(candidate); err == nil {
			if name := strings.TrimSpace(string(content)); name != "" {
				return name
			}
		}
	}
	name, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return name
}

// machineModel 读取宿主机 DMI 产品名（安装脚本只读挂载），用于界面展示机型。
func (c *Collector) machineModel() string {
	content, err := os.ReadFile(filepath.Join(c.hostRoot, "dmi", "product_name"))
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(strings.TrimSpace(string(content)), "_", " ")
}

func (c *Collector) osName() string {
	for _, candidate := range []string{
		filepath.Join(c.hostRoot, "etc", "os-release"),
		"/etc/os-release",
	} {
		if content, err := os.ReadFile(candidate); err == nil {
			for _, line := range strings.Split(string(content), "\n") {
				if value, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
					return strings.Trim(strings.TrimSpace(value), `"`)
				}
			}
		}
	}
	return runtime.GOOS
}

func normalizeArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "arm64"
	default:
		return goarch
	}
}

func (c *Collector) cpuModel() string {
	content, err := os.ReadFile(filepath.Join(c.procRoot, "cpuinfo"))
	if err != nil {
		return "未知处理器"
	}
	for _, line := range strings.Split(string(content), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "model name", "Model", "Hardware":
			if model := strings.TrimSpace(value); model != "" {
				return model
			}
		}
	}
	if runtime.GOARCH == "arm64" {
		return "ARM64 处理器"
	}
	return "未知处理器"
}

func (c *Collector) cpuCores() int {
	content, err := os.ReadFile(filepath.Join(c.procRoot, "cpuinfo"))
	if err == nil {
		cores := 0
		for _, line := range strings.Split(string(content), "\n") {
			if strings.HasPrefix(line, "processor") {
				cores++
			}
		}
		if cores > 0 {
			return cores
		}
	}
	return runtime.NumCPU()
}

// cpuUsage 基于 /proc/stat 两次采样差值；首次调用返回 nil。
func (c *Collector) cpuUsage() *float64 {
	content, err := os.ReadFile(filepath.Join(c.procRoot, "stat"))
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(content), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		if len(fields) < 5 {
			return nil
		}
		var total, idle uint64
		for index, field := range fields {
			value, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return nil
			}
			total += value
			if index == 3 || index == 4 { // idle + iowait
				idle += value
			}
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if !c.prevValid || total <= c.prevTotal {
			c.prevIdle, c.prevTotal, c.prevValid = idle, total, true
			return nil
		}
		idleDelta := float64(idle - c.prevIdle)
		totalDelta := float64(total - c.prevTotal)
		c.prevIdle, c.prevTotal = idle, total
		usage := (1 - idleDelta/totalDelta) * 100
		// 计数器抖动或 cgroup 统计边界可能产生越界值，统一钳制。
		usage = max(0, min(100, usage))
		usage = float64(int(usage*10+0.5)) / 10
		return &usage
	}
	return nil
}

func (c *Collector) memory() (totalMB, usedMB uint64) {
	content, err := os.ReadFile(filepath.Join(c.procRoot, "meminfo"))
	if err != nil {
		return 0, 0
	}
	var totalKB, availableKB uint64
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			totalKB = value
		case "MemAvailable:":
			availableKB = value
		}
	}
	totalMB = totalKB / 1024
	if totalKB > availableKB {
		usedMB = (totalKB - availableKB) / 1024
	}
	return totalMB, usedMB
}

func (c *Collector) storage() (path string, totalBytes, usedBytes uint64) {
	for _, candidate := range []string{c.dataDir, "/"} {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(candidate, &stat); err != nil {
			continue
		}
		total := stat.Blocks * uint64(stat.Bsize)
		available := stat.Bavail * uint64(stat.Bsize)
		used := uint64(0)
		if total > available {
			used = total - available
		}
		return candidate, total, used
	}
	return c.dataDir, 0, 0
}

const nvidiaSMIQuery = "index,name,memory.total,memory.used,utilization.gpu,temperature.gpu,power.draw,power.limit"

func (c *Collector) execNvidiaSMI(ctx context.Context) (string, error) {
	if c.nvidiaSMIBin == "" {
		return "", fmt.Errorf("nvidia-smi 已禁用")
	}
	commandContext, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	output, err := exec.CommandContext(commandContext, c.nvidiaSMIBin,
		"--query-gpu="+nvidiaSMIQuery, "--format=csv,noheader,nounits").Output()
	if err != nil {
		return "", fmt.Errorf("执行 nvidia-smi 失败：%w", err)
	}
	return string(output), nil
}

// execNvidiaSMIHeader 执行不带参数的 nvidia-smi，读取头部驱动与 CUDA 版本行。
func (c *Collector) execNvidiaSMIHeader(ctx context.Context) (string, error) {
	if c.nvidiaSMIBin == "" {
		return "", fmt.Errorf("nvidia-smi 已禁用")
	}
	commandContext, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	output, err := exec.CommandContext(commandContext, c.nvidiaSMIBin).Output()
	if err != nil {
		return "", fmt.Errorf("执行 nvidia-smi 失败：%w", err)
	}
	return string(output), nil
}

var (
	driverVersionPattern = regexp.MustCompile(`Driver Version:\s*([0-9.]+)`)
	cudaVersionPattern   = regexp.MustCompile(`CUDA Version:\s*([0-9.]+)`)
)

// versions 解析 nvidia-smi 头部的驱动与 CUDA 版本；失败时返回空串而非报错。
func (c *Collector) versions(ctx context.Context) (string, string) {
	if c.queryVersions == nil {
		return "", ""
	}
	output, err := c.queryVersions(ctx)
	if err != nil {
		return "", ""
	}
	return parseVersions(output)
}

// parseVersions 从 nvidia-smi 完整输出中提取驱动与 CUDA 版本。
func parseVersions(output string) (string, string) {
	driver, cuda := "", ""
	if match := driverVersionPattern.FindStringSubmatch(output); len(match) == 2 {
		driver = match[1]
	}
	if match := cudaVersionPattern.FindStringSubmatch(output); len(match) == 2 {
		cuda = match[1]
	}
	return driver, cuda
}

func (c *Collector) gpus(ctx context.Context) ([]GPU, bool, string, string) {
	output, err := c.queryGPU(ctx)
	if err != nil {
		return []GPU{}, false, "none", err.Error()
	}
	devices, err := parseNvidiaSMI(output)
	if err != nil {
		return []GPU{}, false, "none", err.Error()
	}
	if len(devices) == 0 {
		return []GPU{}, false, "none", "nvidia-smi 未报告任何 GPU"
	}
	mode := "discrete"
	for _, device := range devices {
		if device.MemoryTotalMB == nil {
			mode = "unified"
			break
		}
	}
	return devices, true, mode, ""
}

// parseNvidiaSMI 解析 csv,noheader,nounits 输出；"[N/A]" 解析为 nil。
func parseNvidiaSMI(output string) ([]GPU, error) {
	devices := []GPU{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 8 {
			return nil, fmt.Errorf("nvidia-smi 输出字段不足：%q", line)
		}
		index, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil {
			return nil, fmt.Errorf("nvidia-smi GPU 序号无法解析：%w", err)
		}
		devices = append(devices, GPU{
			Index:              index,
			Name:               strings.TrimSpace(fields[1]),
			MemoryTotalMB:      parseOptionalFloat(fields[2]),
			MemoryUsedMB:       parseOptionalFloat(fields[3]),
			UtilizationPercent: parseOptionalFloat(fields[4]),
			TemperatureC:       parseOptionalFloat(fields[5]),
			PowerDrawW:         parseOptionalFloat(fields[6]),
			PowerLimitW:        parseOptionalFloat(fields[7]),
		})
	}
	return devices, nil
}

func parseOptionalFloat(field string) *float64 {
	value := strings.TrimSpace(field)
	if value == "" || strings.EqualFold(value, "[N/A]") || strings.EqualFold(value, "N/A") {
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil
	}
	return &parsed
}
