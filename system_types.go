package main

// SystemThresholds holds per-metric warn/critical thresholds returned in the API.
type SystemThresholds struct {
	CPU  ThresholdPair `json:"cpu"`
	RAM  ThresholdPair `json:"ram"`
	Swap ThresholdPair `json:"swap"`
	Disk ThresholdPair `json:"disk"`
}

// ThresholdPair is the JSON shape for a single metric's thresholds.
type ThresholdPair struct {
	Warn     float64 `json:"warn"`
	Critical float64 `json:"critical"`
}

// SystemResponse is the JSON body returned by GET /api/system.
type SystemResponse struct {
	OK          bool             `json:"ok"`
	Degraded    bool             `json:"degraded"`
	Stale       bool             `json:"stale"`
	CollectedAt string           `json:"collectedAt"`
	PollSeconds int              `json:"pollSeconds"`
	Thresholds  SystemThresholds `json:"thresholds"`
	CPU         SystemCPU        `json:"cpu"`
	RAM         SystemRAM        `json:"ram"`
	Swap        SystemSwap       `json:"swap"`
	Disk        SystemDisk       `json:"disk"`
	Versions    SystemVersions   `json:"versions"`
	Errors      []string         `json:"errors,omitempty"`
}

type SystemCPU struct {
	Percent float64 `json:"percent"`
	Cores   int     `json:"cores"`
	Error   *string `json:"error,omitempty"`
}

type SystemRAM struct {
	UsedBytes  int64   `json:"usedBytes"`
	TotalBytes int64   `json:"totalBytes"`
	Percent    float64 `json:"percent"`
	Error      *string `json:"error,omitempty"`
}

type SystemSwap struct {
	UsedBytes  int64   `json:"usedBytes"`
	TotalBytes int64   `json:"totalBytes"`
	Percent    float64 `json:"percent"`
	Error      *string `json:"error,omitempty"`
}

type SystemDisk struct {
	Path       string  `json:"path"`
	UsedBytes  int64   `json:"usedBytes"`
	TotalBytes int64   `json:"totalBytes"`
	Percent    float64 `json:"percent"`
	Error      *string `json:"error,omitempty"`
}

type SystemGateway struct {
	Version string  `json:"version"`
	Status  string  `json:"status"`
	PID     int     `json:"pid,omitempty"`
	Uptime  string  `json:"uptime,omitempty"`
	Memory  string  `json:"memory,omitempty"`
	Error   *string `json:"error,omitempty"`
}

type SystemVersions struct {
	Dashboard string        `json:"dashboard"`
	Openclaw  string        `json:"openclaw"`
	Gateway   SystemGateway `json:"gateway"`
}
