package hardware

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const dgxSparkSMI = "0, NVIDIA GB10, [N/A], [N/A], 0, 38, 10.82, [N/A]\n"

const dualL40SSMI = `0, NVIDIA L40S, 46068, 16300, 38, 53, 93.5, 300
1, NVIDIA L40S, 46068, 20500, 55, 58, 128.2, 300
`

func TestParseDGXSparkUnifiedMemory(t *testing.T) {
	devices, err := parseNvidiaSMI(dgxSparkSMI)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 GPU, got %d", len(devices))
	}
	gpu := devices[0]
	if gpu.Name != "NVIDIA GB10" {
		t.Fatalf("unexpected GPU name %q", gpu.Name)
	}
	if gpu.MemoryTotalMB != nil || gpu.MemoryUsedMB != nil {
		t.Fatal("unified-memory GPU should report nil memory fields")
	}
	if gpu.UtilizationPercent == nil || *gpu.UtilizationPercent != 0 {
		t.Fatalf("unexpected utilization %+v", gpu.UtilizationPercent)
	}
	if gpu.TemperatureC == nil || *gpu.TemperatureC != 38 {
		t.Fatalf("unexpected temperature %+v", gpu.TemperatureC)
	}
	if gpu.PowerDrawW == nil || *gpu.PowerDrawW != 10.82 {
		t.Fatalf("unexpected power draw %+v", gpu.PowerDrawW)
	}
	if gpu.PowerLimitW != nil {
		t.Fatal("GB10 power limit should be nil")
	}
}

func TestParseDiscreteGPUs(t *testing.T) {
	devices, err := parseNvidiaSMI(dualL40SSMI)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("expected 2 GPUs, got %d", len(devices))
	}
	if devices[1].MemoryTotalMB == nil || *devices[1].MemoryTotalMB != 46068 {
		t.Fatalf("unexpected memory total %+v", devices[1].MemoryTotalMB)
	}
	if devices[0].PowerLimitW == nil || *devices[0].PowerLimitW != 300 {
		t.Fatalf("unexpected power limit %+v", devices[0].PowerLimitW)
	}
}

func TestParseRejectsMalformedLine(t *testing.T) {
	if _, err := parseNvidiaSMI("0, NVIDIA L40S, 46068\n"); err == nil {
		t.Fatal("expected error for short row")
	}
}

func writeFixture(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newFixtureCollector(t *testing.T, gpuOutput string) (*Collector, string) {
	t.Helper()
	root := t.TempDir()
	proc := filepath.Join(root, "proc")
	host := filepath.Join(root, "host")
	writeFixture(t, proc, "meminfo", "MemTotal:       131072000 kB\nMemFree:         1000000 kB\nMemAvailable:    65536000 kB\n")
	writeFixture(t, proc, "cpuinfo", "processor\t: 0\nmodel name\t: Test CPU\n\nprocessor\t: 1\nmodel name\t: Test CPU\n")
	writeFixture(t, host, "etc/hostname", "test-host-01\n")
	writeFixture(t, host, "etc/os-release", "NAME=\"Ubuntu\"\nPRETTY_NAME=\"Ubuntu 24.04.4 LTS\"\n")
	writeFixture(t, host, "dmi/product_name", "NVIDIA_DGX_Spark\n")
	collector := NewCollector(Config{ProcRoot: proc, HostRoot: host, DataDir: root, NvidiaSMIBin: "nvidia-smi"})
	collector.queryGPU = func(context.Context) (string, error) { return gpuOutput, nil }
	return collector, root
}

func TestCollectHostBasics(t *testing.T) {
	collector, _ := newFixtureCollector(t, dgxSparkSMI)
	report := collector.Collect(context.Background())
	if report.HostName != "test-host-01" {
		t.Fatalf("unexpected hostname %q", report.HostName)
	}
	if report.OS != "Ubuntu 24.04.4 LTS" {
		t.Fatalf("unexpected os %q", report.OS)
	}
	if report.MachineModel != "NVIDIA DGX Spark" {
		t.Fatalf("unexpected machine model %q", report.MachineModel)
	}
	if report.CPUModel != "Test CPU" || report.CPUCores != 2 {
		t.Fatalf("unexpected cpu %q cores %d", report.CPUModel, report.CPUCores)
	}
	if report.MemoryTotalMB != 128000 || report.MemoryUsedMB != 64000 {
		t.Fatalf("unexpected memory total=%d used=%d", report.MemoryTotalMB, report.MemoryUsedMB)
	}
	if report.StoragePath == "" || report.StorageTotalBytes == 0 {
		t.Fatalf("storage stat failed: %+v", report)
	}
	if !report.GPUAvailable || report.GPUMemoryMode != "unified" || len(report.GPUs) != 1 {
		t.Fatalf("unexpected gpu state available=%v mode=%q count=%d", report.GPUAvailable, report.GPUMemoryMode, len(report.GPUs))
	}
	if report.GPUError != "" {
		t.Fatalf("unexpected gpu error %q", report.GPUError)
	}
}

func TestCollectDiscreteMode(t *testing.T) {
	collector, _ := newFixtureCollector(t, dualL40SSMI)
	report := collector.Collect(context.Background())
	if report.GPUMemoryMode != "discrete" || len(report.GPUs) != 2 {
		t.Fatalf("unexpected mode=%q count=%d", report.GPUMemoryMode, len(report.GPUs))
	}
}

func TestCollectGPUFailureDegrades(t *testing.T) {
	collector, _ := newFixtureCollector(t, "")
	collector.queryGPU = func(context.Context) (string, error) { return "", errors.New("executable not found") }
	report := collector.Collect(context.Background())
	if report.GPUAvailable || report.GPUMemoryMode != "none" || report.GPUError == "" {
		t.Fatalf("expected degraded gpu state, got %+v", report)
	}
	if report.MemoryTotalMB == 0 {
		t.Fatal("gpu failure must not affect memory metrics")
	}
}

func TestCPUUsageDelta(t *testing.T) {
	collector, root := newFixtureCollector(t, dgxSparkSMI)
	proc := filepath.Join(root, "proc")
	writeFixture(t, proc, "stat", "cpu  100 0 100 800 0 0 0 0 0 0\n")
	if usage := collector.Collect(context.Background()).CPUUsagePercent; usage != nil {
		t.Fatalf("first sample should be nil, got %v", *usage)
	}
	writeFixture(t, proc, "stat", "cpu  150 0 150 1200 0 0 0 0 0 0\n")
	usage := collector.Collect(context.Background()).CPUUsagePercent
	if usage == nil {
		t.Fatal("second sample should produce usage")
	}
	// total delta = 500, idle delta = 400 → 20%
	if *usage != 20 {
		t.Fatalf("expected 20%% usage, got %v", *usage)
	}
	// 计数器异常（idle 增量超过 total 增量）时必须钳制到 0，不得返回负值。
	writeFixture(t, proc, "stat", "cpu  150 0 150 3000 0 0 0 0 0 0\n")
	clamped := collector.Collect(context.Background()).CPUUsagePercent
	if clamped == nil || *clamped != 0 {
		t.Fatalf("expected clamped 0%% usage, got %v", clamped)
	}
}

func TestCPUModelFallbackOnARM(t *testing.T) {
	collector, root := newFixtureCollector(t, dgxSparkSMI)
	writeFixture(t, filepath.Join(root, "proc"), "cpuinfo", "processor\t: 0\nFeatures\t: fp asimd\n")
	if model := collector.cpuModel(); model != "ARM64 处理器" && model != "未知处理器" {
		t.Fatalf("unexpected fallback model %q", model)
	}
}
