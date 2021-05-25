package prom

// RuntimeInfo include some filed the prometheus API /api/v1/runtimeinfo returned
type RuntimeInfo struct {
	// TimeSeriesCount is the series the prometheus head check handled
	TimeSeriesCount int64 `json:"timeSeriesCount"`
}

// TSDBInfo include some filed the prometheus API /api/v1/status/tsdb returned
type TSDBInfo struct {
	// HeadStats include information of current head block
	HeadStats struct {
		// NumSeries is current series in head block (in memory)
		NumSeries int64 `json:"numSeries"`
	} `json:"headStats"`
}
