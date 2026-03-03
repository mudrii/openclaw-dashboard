package main

// SystemResponse is the JSON body returned by GET /api/system.
type SystemResponse struct {
	OK          bool           `json:"ok"`
	Degraded    bool           `json:"degraded"`
	Stale       bool           `json:"stale"`
	CollectedAt string         `json:"collectedAt"`
	PollSeconds int            `json:"pollSeconds"`
	CPU         SystemCPU      `json:"cpu"`
	RAM         SystemRAM      `json:"ram"`
	Swap        SystemSwap     `json:"swap"`
	Disk        SystemDisk     `json:"disk"`
	Versions    SystemVersions `json:"versions"`
	Errors      []string       `json:"errors,omitempty"`
}

type SystemCPU struct {
	Percent float64 `json:"percent"`
	Cores   int     `json:"cores"`
	Error   *string `json:"error"`
}

type SystemRAM struct {
	UsedBytes  int64   `json:"usedBytes"`
	TotalBytes int64   `json:"totalBytes"`
	Percent    float64 `json:"percent"`
	Error      *string `json:"error"`
}

type SystemSwap struct {
	UsedBytes  int64   `json:"usedBytes"`
	TotalBytes int64   `json:"totalBytes"`
	Percent    float64 `json:"percent"`
	Error      *string `json:"error"`
}

type SystemDisk struct {
	Path       string  `json:"path"`
	UsedBytes  int64   `json:"usedBytes"`
	TotalBytes int64   `json:"totalBytes"`
	Percent    float64 `json:"percent"`
	Error      *string `json:"error"`
}

type SystemGateway struct {
	Version string  `json:"version"`
	Status  string  `json:"status"`
	Error   *string `json:"error"`
}

type SystemVersions struct {
	Dashboard string        `json:"dashboard"`
	Openclaw  string        `json:"openclaw"`
	Gateway   SystemGateway `json:"gateway"`
}
