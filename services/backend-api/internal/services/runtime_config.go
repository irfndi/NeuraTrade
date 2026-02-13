package services

import (
	"log/slog"
	"runtime"
	"runtime/debug"

	"github.com/irfndi/neuratrade/internal/telemetry"
)

type RuntimeConfig struct {
	MaxProcs         int   `json:"max_procs"`
	GCPercent        int   `json:"gc_percent"`
	MemoryLimit      int64 `json:"memory_limit_mb"`
	TraceEnabled     bool  `json:"trace_enabled"`
	BlockProfileRate int   `json:"block_profile_rate"`
	MutexProfileRate int   `json:"mutex_profile_rate"`
}

func DefaultRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		MaxProcs:         0,   // 0 = use all available CPUs
		GCPercent:        100, // Default Go GC percentage
		MemoryLimit:      0,   // 0 = no limit
		TraceEnabled:     false,
		BlockProfileRate: 0,
		MutexProfileRate: 0,
	}
}

type RuntimeOptimizer struct {
	config RuntimeConfig
	logger *slog.Logger
}

func NewRuntimeOptimizer(config RuntimeConfig) *RuntimeOptimizer {
	return &RuntimeOptimizer{
		config: config,
		logger: telemetry.Logger(),
	}
}

func (ro *RuntimeOptimizer) Apply() {
	ro.configureMaxProcs()
	ro.configureGC()
	ro.configureMemoryLimit()
	ro.configureProfiling()
	ro.logConfiguration()
}

func (ro *RuntimeOptimizer) configureMaxProcs() {
	previous := runtime.GOMAXPROCS(ro.config.MaxProcs)
	current := runtime.GOMAXPROCS(0)

	ro.logger.Info("Configured GOMAXPROCS",
		"previous", previous,
		"current", current,
		"requested", ro.config.MaxProcs,
	)
}

func (ro *RuntimeOptimizer) configureGC() {
	if ro.config.GCPercent > 0 {
		previous := debug.SetGCPercent(ro.config.GCPercent)
		ro.logger.Info("Configured GC percentage",
			"previous", previous,
			"current", ro.config.GCPercent,
		)
	}
}

func (ro *RuntimeOptimizer) configureMemoryLimit() {
	if ro.config.MemoryLimit > 0 {
		memoryLimitBytes := ro.config.MemoryLimit * 1024 * 1024
		previous := debug.SetMemoryLimit(memoryLimitBytes)
		ro.logger.Info("Configured memory limit",
			"previous_mb", previous/1024/1024,
			"current_mb", ro.config.MemoryLimit,
		)
	}
}

func (ro *RuntimeOptimizer) configureProfiling() {
	if ro.config.BlockProfileRate > 0 {
		runtime.SetBlockProfileRate(ro.config.BlockProfileRate)
		ro.logger.Info("Enabled block profiling", "rate", ro.config.BlockProfileRate)
	}

	if ro.config.MutexProfileRate > 0 {
		previous := runtime.SetMutexProfileFraction(ro.config.MutexProfileRate)
		ro.logger.Info("Configured mutex profiling",
			"previous", previous,
			"current", ro.config.MutexProfileRate,
		)
	}
}

func (ro *RuntimeOptimizer) logConfiguration() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	ro.logger.Info("Runtime configuration applied",
		"gomaxprocs", runtime.GOMAXPROCS(0),
		"num_cpu", runtime.NumCPU(),
		"num_goroutine", runtime.NumGoroutine(),
		"go_version", runtime.Version(),
		"gc_percent", ro.config.GCPercent,
		"memory_limit_mb", ro.config.MemoryLimit,
		"heap_alloc_mb", memStats.HeapAlloc/1024/1024,
		"heap_sys_mb", memStats.HeapSys/1024/1024,
		"stack_inuse_mb", memStats.StackInuse/1024/1024,
	)
}

func (ro *RuntimeOptimizer) GetConfig() RuntimeConfig {
	return ro.config
}

func (ro *RuntimeOptimizer) GetRuntimeStats() RuntimeStats {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return RuntimeStats{
		GOMAXPROCS:    runtime.GOMAXPROCS(0),
		NumCPU:        runtime.NumCPU(),
		NumGoroutine:  runtime.NumGoroutine(),
		GoVersion:     runtime.Version(),
		HeapAlloc:     memStats.HeapAlloc,
		HeapSys:       memStats.HeapSys,
		HeapInuse:     memStats.HeapInuse,
		HeapIdle:      memStats.HeapIdle,
		HeapReleased:  memStats.HeapReleased,
		StackInuse:    memStats.StackInuse,
		StackSys:      memStats.StackSys,
		MSpanInuse:    memStats.MSpanInuse,
		MSpanSys:      memStats.MSpanSys,
		MCacheInuse:   memStats.MCacheInuse,
		MCacheSys:     memStats.MCacheSys,
		BuckHashSys:   memStats.BuckHashSys,
		GCSys:         memStats.GCSys,
		OtherSys:      memStats.OtherSys,
		NextGC:        memStats.NextGC,
		LastGC:        memStats.LastGC,
		PauseTotalNs:  memStats.PauseTotalNs,
		NumGC:         memStats.NumGC,
		NumForcedGC:   memStats.NumForcedGC,
		GCCPUFraction: memStats.GCCPUFraction,
	}
}

type RuntimeStats struct {
	GOMAXPROCS    int     `json:"gomaxprocs"`
	NumCPU        int     `json:"num_cpu"`
	NumGoroutine  int     `json:"num_goroutine"`
	GoVersion     string  `json:"go_version"`
	HeapAlloc     uint64  `json:"heap_alloc"`
	HeapSys       uint64  `json:"heap_sys"`
	HeapInuse     uint64  `json:"heap_inuse"`
	HeapIdle      uint64  `json:"heap_idle"`
	HeapReleased  uint64  `json:"heap_released"`
	StackInuse    uint64  `json:"stack_inuse"`
	StackSys      uint64  `json:"stack_sys"`
	MSpanInuse    uint64  `json:"mspan_inuse"`
	MSpanSys      uint64  `json:"mspan_sys"`
	MCacheInuse   uint64  `json:"mcache_inuse"`
	MCacheSys     uint64  `json:"mcache_sys"`
	BuckHashSys   uint64  `json:"buck_hash_sys"`
	GCSys         uint64  `json:"gc_sys"`
	OtherSys      uint64  `json:"other_sys"`
	NextGC        uint64  `json:"next_gc"`
	LastGC        uint64  `json:"last_gc"`
	PauseTotalNs  uint64  `json:"pause_total_ns"`
	NumGC         uint32  `json:"num_gc"`
	NumForcedGC   uint32  `json:"num_forced_gc"`
	GCCPUFraction float64 `json:"gc_cpu_fraction"`
}

func ForceGC() {
	runtime.GC()
}

func GetMemoryPressure() float64 {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	if memStats.HeapSys == 0 {
		return 0
	}
	return float64(memStats.HeapInuse) / float64(memStats.HeapSys)
}

func GetRecommendedWorkerCount() int {
	cpuCount := runtime.NumCPU()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	availableMB := memStats.HeapSys / 1024 / 1024

	switch {
	case availableMB >= 8192:
		return min(cpuCount*4, 32)
	case availableMB >= 4096:
		return min(cpuCount*3, 24)
	case availableMB >= 2048:
		return min(cpuCount*2, 16)
	default:
		return max(cpuCount, 4)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
