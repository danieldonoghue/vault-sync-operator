// Package goruntime provides utilities for Go runtime configuration and memory management.
package goruntime

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"

	"github.com/danieldonoghue/vault-sync-operator/internal/metrics"
	"github.com/go-logr/logr"
)

const UnsetValue = "unset"

// LogRuntimeConfiguration logs the current Go runtime configuration
// This is useful for verifying that GOMAXPROCS and GOMEMLIMIT are set correctly in containers
func LogRuntimeConfiguration(log logr.Logger) {
	maxProcs := runtime.GOMAXPROCS(0)
	memLimit := getGOMEMLIMIT()

	log.Info("Go runtime configuration",
		"GOMAXPROCS", maxProcs,
		"NumCPU", runtime.NumCPU(),
		"GOMEMLIMIT", memLimit,
		"container_memory_limit", getContainerMemoryLimit(),
		"container_cpu_limit", getContainerCPULimit(),
	)

	// Update Prometheus metrics
	metrics.RuntimeInfo.WithLabelValues("gomaxprocs", fmt.Sprintf("%d", maxProcs)).Set(float64(maxProcs))
	metrics.RuntimeInfo.WithLabelValues("numcpu", fmt.Sprintf("%d", runtime.NumCPU())).Set(float64(runtime.NumCPU()))
	if memLimit != UnsetValue {
		if memBytes, err := ParseMemoryLimit(memLimit); err == nil {
			metrics.RuntimeInfo.WithLabelValues("gomemlimit_bytes", memLimit).Set(float64(memBytes))
		}
	}

	// Log GC settings
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "GOGC" {
				log.Info("Go GC configuration", "GOGC", setting.Value)
				metrics.RuntimeInfo.WithLabelValues("gogc", setting.Value).Set(1)
				break
			}
		}
	}
}

// getGOMEMLIMIT returns the current GOMEMLIMIT setting
func getGOMEMLIMIT() string {
	if limit := os.Getenv("GOMEMLIMIT"); limit != "" {
		return limit
	}
	return UnsetValue
}

// getContainerMemoryLimit returns the memory limit from environment
func getContainerMemoryLimit() string {
	if limit := os.Getenv("GOMEMLIMIT"); limit != "" {
		return limit
	}
	return UnsetValue
}

// getContainerCPULimit returns the CPU limit from environment
func getContainerCPULimit() string {
	if limit := os.Getenv("GOMAXPROCS"); limit != "" {
		return limit
	}
	return fmt.Sprintf("auto-detected=%d", runtime.GOMAXPROCS(0))
}

// ValidateRuntimeConfiguration validates that the runtime is properly configured for containers
func ValidateRuntimeConfiguration(log logr.Logger) {
	warnings := []string{}

	// Check GOMAXPROCS
	maxProcs := runtime.GOMAXPROCS(0)
	numCPU := runtime.NumCPU()

	// If GOMAXPROCS equals NumCPU, it might not be container-aware
	if maxProcs == numCPU {
		if os.Getenv("GOMAXPROCS") == "" {
			warnings = append(warnings, "GOMAXPROCS appears to use host CPU count instead of container limits")
		}
	}

	// Check GOMEMLIMIT
	if os.Getenv("GOMEMLIMIT") == "" {
		warnings = append(warnings, "GOMEMLIMIT not set - Go GC may not respect container memory limits")
	}

	// Log warnings
	for _, warning := range warnings {
		log.Info("Runtime configuration warning", "warning", warning)
	}

	if len(warnings) == 0 {
		log.Info("Runtime configuration validation passed")
	}
}

// ParseMemoryLimit parses a memory limit string (e.g., "128Mi") to bytes
func ParseMemoryLimit(limit string) (int64, error) {
	if limit == "" {
		return 0, fmt.Errorf("empty memory limit")
	}

	// Handle common Kubernetes memory suffixes
	multiplier := int64(1)
	numStr := limit

	if len(limit) >= 2 {
		suffix := limit[len(limit)-2:]
		switch suffix {
		case "Ki":
			multiplier = 1024
			numStr = limit[:len(limit)-2]
		case "Mi":
			multiplier = 1024 * 1024
			numStr = limit[:len(limit)-2]
		case "Gi":
			multiplier = 1024 * 1024 * 1024
			numStr = limit[:len(limit)-2]
		}
	}

	num, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse memory limit %s: %w", limit, err)
	}

	return num * multiplier, nil
}
