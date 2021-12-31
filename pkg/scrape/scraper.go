package scrape

import (
	"context"
	"fmt"
	"io"

	"github.com/klauspost/compress/gzip"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/protoparser/common"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/relabel"
	"github.com/sirupsen/logrus"

	"net/http"
	"time"

	parser "github.com/VictoriaMetrics/VictoriaMetrics/lib/protoparser/prometheus"
)

// Scraper do one scraping
// must call RequestTo befor ParseResponse
type Scraper struct {
	job    *JobInfo
	url    string
	writer []io.Writer
	log    logrus.FieldLogger
	// HTTPResponse save the http reponse when RequestTo is called
	HTTPResponse *http.Response
	gZipReader   *gzip.Reader
	reader       io.ReadCloser
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
	defer cancel()

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

// StatisticSeries statistic load from metrics raw data
func StatisticSeries(rows []parser.Row, rc []*relabel.Config) int64 {
	total := int64(0)
	for _, row := range rows {
		var lset labels.Labels
		lset = append(lset, labels.Label{
			Name:  "__name__",
			Value: row.Metric,
		})
		for _, tag := range row.Tags {
			lset = append(lset, labels.Label{
				Name:  tag.Key,
				Value: tag.Value,
			})
		}
		if newSets := relabel.Process(lset, rc...); newSets != nil {
			total++
		}
	}
	return total
}

func init() {
	common.StartUnmarshalWorkers()
}
