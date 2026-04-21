package sandbox

// ResourceLimits defines the constraints for sandbox execution.
type ResourceLimits struct {
	// CPUQuota in microseconds per CPU period (default 50000 = 0.5 cores).
	CPUQuota int64 `json:"cpu_quota"`

	// MemoryBytes is the hard memory limit in bytes (default 256MB).
	MemoryBytes int64 `json:"memory_bytes"`

	// NetworkDisabled disables all network access (default true).
	NetworkDisabled bool `json:"network_disabled"`

	// TimeoutSec is the maximum execution time in seconds (default 30).
	TimeoutSec int `json:"timeout_sec"`

	// MaxOutputBytes limits the stdout capture size (default 1MB).
	MaxOutputBytes int64 `json:"max_output_bytes"`

	// ReadOnlyRootfs makes the root filesystem read-only (default true).
	ReadOnlyRootfs bool `json:"read_only_rootfs"`

	// PidsLimit restricts the number of processes (default 50).
	PidsLimit int64 `json:"pids_limit"`
}

// DefaultLimits returns the default resource limits.
func DefaultLimits() ResourceLimits {
	return ResourceLimits{
		CPUQuota:        50000,            // 0.5 CPU cores
		MemoryBytes:     256 * 1024 * 1024, // 256 MB
		NetworkDisabled: true,
		TimeoutSec:      30,
		MaxOutputBytes:  1024 * 1024,       // 1 MB
		ReadOnlyRootfs:  true,
		PidsLimit:       50,
	}
}

// Merge applies non-zero overrides onto the defaults.
func (l ResourceLimits) Merge(override *ResourceLimits) ResourceLimits {
	if override == nil {
		return l
	}
	if override.CPUQuota > 0 {
		l.CPUQuota = override.CPUQuota
	}
	if override.MemoryBytes > 0 {
		l.MemoryBytes = override.MemoryBytes
	}
	if override.TimeoutSec > 0 {
		l.TimeoutSec = override.TimeoutSec
	}
	if override.MaxOutputBytes > 0 {
		l.MaxOutputBytes = override.MaxOutputBytes
	}
	if override.PidsLimit > 0 {
		l.PidsLimit = override.PidsLimit
	}
	// Booleans: use override value explicitly
	l.NetworkDisabled = override.NetworkDisabled
	l.ReadOnlyRootfs = override.ReadOnlyRootfs
	return l
}
