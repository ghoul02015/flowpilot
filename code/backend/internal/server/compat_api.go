package server

import (
	"net/http"
)

// CompatItem 兼容性检查项。
type CompatItem struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Result string `json:"result"` // pass / warn / block
	Detail string `json:"detail"`
	Advice string `json:"advice,omitempty"`
}

// hardwareCompat 硬件与环境兼容性检查（只读判定＋修复建议，不自动修改）。
func (h *Handler) hardwareCompat(w http.ResponseWriter, r *http.Request) {
	items := []CompatItem{}
	add := func(id, name, result, detail, advice string) {
		items = append(items, CompatItem{ID: id, Name: name, Result: result, Detail: detail, Advice: advice})
	}

	// 硬件报告（只读判定；GPU 采集失败是检查项之一）。
	report := h.hardware.Collect(r.Context())
	if report.GPUError != "" && len(report.GPUs) == 0 {
		add("hardware", "硬件采集", "warn", "硬件采集部分失败："+report.GPUError, "纯 CPU 部署可忽略；GPU 服务器请检查 NVIDIA 驱动与 Container Toolkit")
	} else {
		add("hardware", "硬件采集", "pass", "硬件采集正常", "")
	}

	// Docker 信息。
	engine, err := h.docker.Engine(r.Context())
	if err != nil {
		add("docker", "Docker Engine", "block", "Docker Engine 信息读取失败："+err.Error(), "确认 Docker 服务与 socket 挂载")
	} else {
		minorOK := engine.Version.Version != ""
		add("docker", "Docker Engine", "pass", "Docker "+engine.Version.Version+" · 容器 "+itoa(engine.Info.Containers)+" 个", "")
		if !minorOK {
			add("docker", "Docker Engine", "warn", "无法解析 Docker 版本", "建议 Docker 24+")
		}
	}

	// GPU 与 NVIDIA runtime。
	if len(report.GPUs) > 0 {
		// NVIDIA runtime 判定：GPU 存在且 nvidia-smi 成功视为 runtime 可用（管理容器已注入）。
		if report.GPUError != "" {
			add("nvidia", "NVIDIA 驱动", "block", "GPU 存在但采集失败："+report.GPUError, "检查 nvidia-smi 与 NVIDIA Container Toolkit")
		} else {
			add("nvidia", "NVIDIA 驱动", "pass", "驱动 "+report.DriverVersion+" · CUDA "+report.CUDAVersion, "")
			// 高温检查。
			hot := ""
			for _, gpu := range report.GPUs {
				if gpu.TemperatureC != nil && *gpu.TemperatureC > 85 {
					hot += gpu.Name + " " + ftoa(*gpu.TemperatureC) + "°C；"
				}
			}
			if hot != "" {
				add("temperature", "GPU 温度", "warn", "高温："+hot, "检查散热与机箱风道")
			} else {
				add("temperature", "GPU 温度", "pass", "全部 GPU ≤85°C", "")
			}
		}
		if report.GPUMemoryMode == "unified" {
			add("gpu-memory", "GPU 显存模式", "pass", "统一内存架构（系统内存共享，非独立显存）", "容量评估请按统一内存口径")
		}
	} else if report.GPUError != "" {
		// 有错误时明确区分“采集失败”与“确实没有 GPU”，避免误导排障。
		add("nvidia", "NVIDIA GPU", "warn", "GPU 采集失败："+report.GPUError, "检查 nvidia-smi 是否可执行；GPU 服务器需安装驱动并挂载 NVIDIA runtime")
	} else {
		add("nvidia", "NVIDIA GPU", "warn", "未检测到 GPU", "实例将以 CPU 模式运行；GPU 服务器请检查驱动")
	}

	// 磁盘水位。
	if report.StorageTotalBytes > 0 {
		ratio := float64(report.StorageUsedBytes) / float64(report.StorageTotalBytes) * 100
		switch {
		case ratio >= 95:
			add("disk", "数据盘水位", "block", "已用 "+ftoa(ratio)+"%（≥95%，阻断级）", "立即清理模型缓存或扩容")
		case ratio >= 90:
			add("disk", "数据盘水位", "warn", "已用 "+ftoa(ratio)+"%（≥90%）", "建议清理或扩容")
		default:
			add("disk", "数据盘水位", "pass", "已用 "+ftoa(ratio)+"%", "")
		}
	}

	// 架构与 OS。
	if report.OS != "" {
		arch := report.Architecture
		if arch == "arm64" || arch == "amd64" || arch == "aarch64" || arch == "x86_64" {
			add("arch", "系统架构", "pass", report.OS+" · "+arch, "")
		} else {
			add("arch", "系统架构", "warn", "非常见架构 "+arch, "未验证的架构，镜像兼容性需确认")
		}
	}

	// 汇总。
	summary := map[string]int{"pass": 0, "warn": 0, "block": 0}
	for _, item := range items {
		summary[item.Result]++
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "summary": summary})
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

func ftoa(value float64) string {
	whole := int(value)
	frac := int((value - float64(whole)) * 10)
	return itoa(whole) + "." + itoa(frac)
}
