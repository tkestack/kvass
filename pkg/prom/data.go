package prom

// RuntimeInfo include some filed the prometheus API /api/v1/runtimeinfo returned
type RuntimeInfo struct {
	// TimeSeriesCount is the series the prometheus head check handled
	TimeSeriesCount int64 `json:"timeSeriesCount"`
}
