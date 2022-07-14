package scrape

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/klauspost/compress/gzip"
	"tkestack.io/kvass/pkg/utils/types"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/protoparser/common"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/sirupsen/logrus"

	parser "github.com/VictoriaMetrics/VictoriaMetrics/lib/protoparser/prometheus"
)

// Scraper do one scraping
// must call RequestTo befor ParseResponse
type Scraper struct {
	job    *JobInfo
	url    string
	writer []io.Writer
	log    logrus.FieldLogger
	// HTTPResponse save the http response when RequestTo is called
	HTTPResponse *http.Response
	gZipReader   *gzip.Reader
	reader       io.ReadCloser
	ctxCancel    func()
}

// NewScraper create a new Scraper
func NewScraper(job *JobInfo, url string, log logrus.FieldLogger) *Scraper {
	return &Scraper{
		job: job,
		url: url,
		log: log,
	}
}

// WithRawWriter add writers
// data will be copy to writers when ParseResponse is processing
// gziped data will be decoded before write to writer
func (s *Scraper) WithRawWriter(w ...io.Writer) {
	s.writer = append(s.writer, w...)
}

// RequestTo do http request to target
// response will be saved to s.HTTPResponse
// ParseResponse must be called if RequestTo return nil error
func (s *Scraper) RequestTo() error {
	req, err := http.NewRequest("GET", s.url, nil)
	if err != nil {
		return errors.Wrap(err, "new request")
	}
	req.Header.Add("Accept", acceptHeader)
	req.Header.Add("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", userAgentHeader)
	req.Header.Set("X-prometheusURL-Cli-Timeout-Seconds", fmt.Sprintf("%f", time.Duration(s.job.Config.ScrapeTimeout).Seconds()))
	if s.job.proxyURL != nil {
		req.Header.Set("Origin-Proxy", s.job.proxyURL.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.job.Config.ScrapeTimeout))
	s.ctxCancel = cancel

	s.HTTPResponse, err = s.job.Cli.Do(req.WithContext(ctx))
	if err != nil {
		return errors.Wrap(err, "do http")
	}

	if s.HTTPResponse.StatusCode != http.StatusOK {
		return errors.Errorf("server returned HTTP status %s", s.HTTPResponse.Status)
	}

	s.reader = s.HTTPResponse.Body
	if s.HTTPResponse.Header.Get("Content-Encoding") == "gzip" {
		s.gZipReader, err = common.GetGzipReader(s.HTTPResponse.Body)
		if err != nil {
			return fmt.Errorf("cannot read gzipped lines with Prometheus exposition format: %w", err)
		}
		s.reader = s.gZipReader
	}

	s.reader = wrapReader(s.reader, s.writer...)
	return nil
}

// ParseResponse parse metrics
// RequestTo must be called before ParseResponse
func (s *Scraper) ParseResponse(do func(rows []parser.Row) error) error {
	defer func() {
		s.ctxCancel()
		if s.gZipReader != nil {
			common.PutGzipReader(s.gZipReader)
		}
	}()

	return parser.ParseStream(s.reader, time.Now().UnixNano()/1e6,
		false,
		do, func(str string) {
			s.log.Print(str)
		})
}

// StatisticsSeriesResult is the samples count in one scrape
type StatisticsSeriesResult struct {
	lk sync.Mutex `json:"-"`
	// ScrapedTotal is samples number total after relabel
	ScrapedTotal float64 `json:"scrapedTotal"`
	// Total is total samples appeared in this scape
	Total float64 `json:"total"`
	// MetricsTotal is samples number info about all metrics
	MetricsTotal map[string]*MetricSamplesInfo `json:"metricsTotal"`
}

// NewStatisticsSeriesResult return an empty StatisticsSeriesResult
func NewStatisticsSeriesResult() *StatisticsSeriesResult {
	return &StatisticsSeriesResult{
		MetricsTotal: map[string]*MetricSamplesInfo{},
	}
}

// MetricSamplesInfo statistics sample about one metric
type MetricSamplesInfo struct {
	// Total is total samples appeared in this scape
	Total float64 `json:"total"`
	// Scraped is samples number after relabel
	Scraped float64 `json:"scraped"`
}

// StatisticSeries statistic load from metrics raw data
func StatisticSeries(rows []parser.Row, rc []*relabel.Config, result *StatisticsSeriesResult) {
	result.lk.Lock()
	defer result.lk.Unlock()

	for _, row := range rows {
		var lset labels.Labels
		lset = append(lset, labels.Label{
			Name:  "__name__",
			Value: row.Metric,
		})

		n := types.DeepCopyString(row.Metric)
		if result.MetricsTotal[n] == nil {
			result.MetricsTotal[n] = &MetricSamplesInfo{}
		}

		result.MetricsTotal[n].Total++
		for _, tag := range row.Tags {
			lset = append(lset, labels.Label{
				Name:  tag.Key,
				Value: tag.Value,
			})
		}

		result.Total++
		if newSets := relabel.Process(lset, rc...); newSets != nil {
			result.ScrapedTotal++
			result.MetricsTotal[n].Scraped++
		}
	}
}

func init() {
	common.StartUnmarshalWorkers()
}
