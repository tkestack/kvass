package target

import (
	"github.com/prometheus/prometheus/scrape"
	"time"
)

// ScrapeStatus contains last scraping status of the target
type ScrapeStatus struct {
	// LastError save the error string if last scraping is error
	LastError string `json:"lastError"`
	// LastScrape save the time of last scraping
	LastScrape time.Time `json:"lastScrape"`
	// LastScrapeDuration save the seconds duration last scraping spend
	LastScrapeDuration float64 `json:"lastScrapeDuration"`
	// Health it the status of last scraping
	Health scrape.TargetHealth `json:"health"`
	// Series is the avg sample of last 3 times scraping, metrics_relabel_configs will be process
	Series     int64 `json:"samples"`
	lastSeries []int64
}

func (t *ScrapeStatus) SetScrapeErr(start time.Time, err error) {
	t.LastScrape = start
	t.LastScrapeDuration = time.Now().Sub(start).Seconds()
	if err == nil {
		t.LastError = ""
		t.Health = scrape.HealthGood
	} else {
		t.LastError = err.Error()
		t.Health = scrape.HealthBad
	}
}

// NewScrapeStatus create a new ScrapeStatus with referential Series
func NewScrapeStatus(series int64) *ScrapeStatus {
	return &ScrapeStatus{
		Series: series,
		Health: scrape.HealthUnknown,
	}
}

// UpdateSamples statistic target samples info
func (t *ScrapeStatus) UpdateSamples(s int64) {
	if len(t.lastSeries) < 3 {
		t.lastSeries = append(t.lastSeries, s)
	} else {
		newSeries := make([]int64, 0)
		newSeries = append(newSeries, t.lastSeries[1:]...)
		newSeries = append(newSeries, s)
		t.lastSeries = newSeries
	}

	total := int64(0)
	for _, i := range t.lastSeries {
		total += i
	}

	t.Series = int64(float64(total) / float64(len(t.lastSeries)))
}
