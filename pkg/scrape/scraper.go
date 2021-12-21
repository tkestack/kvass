package scrape

import (
	"context"
	"fmt"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/protoparser/common"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/relabel"
	"github.com/sirupsen/logrus"
	"io"

	parser "github.com/VictoriaMetrics/VictoriaMetrics/lib/protoparser/prometheus"
	"net/http"
	"time"
)

type Scraper struct {
	job        *JobInfo
	url        string
	writer     []io.Writer
	bodyReader io.ReadCloser
	log        logrus.FieldLogger
	isGzip     bool
}

func NewScraper(job *JobInfo, url string, log logrus.FieldLogger) *Scraper {
	return &Scraper{
		job: job,
		url: url,
		log: log,
	}
}

func (s *Scraper) WithRawWriter(w ...io.Writer) {
	s.writer = append(s.writer, w...)
}

func (s *Scraper) RequestTo() (string, error) {
	req, err := http.NewRequest("GET", s.url, nil)
	if err != nil {
		return "", errors.Wrap(err, "new request")
	}
	req.Header.Add("Accept", acceptHeader)
	req.Header.Add("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", userAgentHeader)
	req.Header.Set("X-prometheusURL-Cli-Timeout-Seconds", fmt.Sprintf("%f", time.Duration(s.job.Config.ScrapeTimeout).Seconds()))
	if s.job.proxyURL != nil {
		req.Header.Set("Origin-Proxy", s.job.proxyURL.String())
	}

	ctx, _ := context.WithTimeout(context.Background(), time.Duration(s.job.Config.ScrapeTimeout))
	resp, err := s.job.Cli.Do(req.WithContext(ctx))
	if err != nil {
		return "", errors.Wrap(err, "do http")
	}

	if resp.StatusCode != http.StatusOK {
		return "", errors.Errorf("server returned HTTP status %s", resp.Status)
	}
	s.bodyReader = wrapReader(resp.Body, s.writer...)
	s.isGzip = resp.Header.Get("Content-Encoding") == "gzip"
	return resp.Header.Get("Content-Type"), nil
}

func (s *Scraper) ParseResponse(do func(rows []parser.Row) error) error {
	return parser.ParseStream(s.bodyReader, time.Now().UnixNano()/1e6, s.isGzip, do, func(str string) {
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
