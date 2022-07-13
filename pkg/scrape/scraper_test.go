package scrape

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/protoparser/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestStatisticSample(t *testing.T) {
	r := NewStatisticsSeriesResult()
	StatisticSeries([]prometheus.Row{
		{
			Metric: "a",
			Tags: []prometheus.Tag{
				{
					Key:   "tk",
					Value: "tv",
				},
			},
		},
		{
			Metric: "b",
			Tags: []prometheus.Tag{
				{
					Key:   "tk1",
					Value: "tv1",
				},
			},
		},
	}, []*relabel.Config{
		{
			SourceLabels: []model.LabelName{"tk"},
			Regex:        relabel.MustNewRegexp("tv"),
			Action:       relabel.Drop,
		},
	}, r)
	require.Equal(t, r, &StatisticsSeriesResult{
		ScrapedTotal: 1,
		MetricsTotal: map[string]*MetricSamplesInfo{
			"a": {
				Total:   1,
				Scraped: 0,
			},
			"b": {
				Total:   1,
				Scraped: 1,
			},
		},
	})
}

func gzippedData(raw []byte) []byte {
	ret := bytes.NewBuffer([]byte{})
	w := gzip.NewWriter(ret)
	_, _ = w.Write(raw)
	w.Close()
	return ret.Bytes()
}

func TestScraper_ParseResponse(t *testing.T) {
	type caseInfo struct {
		statusCode     int
		responseHeader http.Header
		responseData   []byte
		scraperJob     *JobInfo
		parseReponseDo func(rows []prometheus.Row) error

		wantRequestHeader http.Header
		wantRequestToErr  bool
		wantResponseErr   bool
	}

	getJob := func() *JobInfo {
		job, err := newJobInfo(config.ScrapeConfig{
			JobName:       "test",
			ScrapeTimeout: model.Duration(time.Second),
		}, false)
		if err != nil {
			require.Fail(t, err.Error())
		}
		return job
	}

	success := func() *caseInfo {
		return &caseInfo{
			statusCode:     200,
			responseHeader: http.Header{},
			responseData:   []byte("metrics{} 0"),
			scraperJob:     getJob(),
			wantRequestHeader: http.Header{
				"Accept":          []string{acceptHeader},
				"Accept-Encoding": []string{"gzip"}, // must support gzip data
			},
			parseReponseDo: func(rows []prometheus.Row) error {
				if len(rows) != 1 {
					return fmt.Errorf("want len 1")
				}
				return nil
			},
		}
	}

	var cases = []struct {
		name       string
		updateCase func(c *caseInfo)
	}{
		{
			name:       "must success",
			updateCase: func(c *caseInfo) {},
		},
		{
			name: "if proxy url is set in config, set it to Origin-Proxy",
			updateCase: func(c *caseInfo) {
				url, err := url.ParseRequestURI("http://127.0.0.1")
				if err != nil {
					require.Fail(t, err.Error())
				}
				c.scraperJob.proxyURL = url
				c.wantRequestHeader.Set("Origin-Proxy", url.String())
			},
		},
		{
			name: "with gziped data",
			updateCase: func(c *caseInfo) {
				c.responseHeader.Set("Content-Encoding", "gzip")
				c.responseData = gzippedData(c.responseData)
			},
		},
		{
			name: "return status code != 200, must return err",
			updateCase: func(c *caseInfo) {
				c.statusCode = 400
				c.wantRequestToErr = true
			},
		},
		{
			name: "request timeout, must return err",
			updateCase: func(c *caseInfo) {
				c.scraperJob.Config.ScrapeTimeout = 0
				c.wantRequestToErr = true
			},
		},
		{
			name: "with worng data, parse response err",
			updateCase: func(c *caseInfo) {
				c.responseData = []byte("123x  x x x")
				c.wantResponseErr = true
			},
		},
		{
			name: "do response deal failed, return err ",
			updateCase: func(c *caseInfo) {
				c.parseReponseDo = func(rows []prometheus.Row) error {
					return fmt.Errorf("test")
				}
				c.wantResponseErr = true
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := require.New(t)
			cs := success()
			c.updateCase(cs)

			targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				for k := range cs.wantRequestHeader {
					if req.Header.Get(k) == "" {
						r.Failf("want head", k)
					}
				}

				for k := range cs.responseHeader {
					w.Header().Add(k, cs.responseHeader.Get(k))
				}

				w.WriteHeader(cs.statusCode)
				_, err := w.Write(cs.responseData)
				r.NoError(err)
			}))
			defer targetServer.Close()

			s := NewScraper(cs.scraperJob, targetServer.URL, logrus.New())
			err := s.RequestTo()
			if err != nil {
				t.Log(err.Error())
			}
			r.Equal(cs.wantRequestToErr, err != nil)
			if cs.wantRequestToErr {
				return
			}

			err = s.ParseResponse(cs.parseReponseDo)
			if err != nil {
				t.Log(err.Error())
			}
			r.Equal(cs.wantResponseErr, err != nil)
		})
	}
}

func TestScraper_WithRawWriter(t *testing.T) {
	s := NewScraper(nil, "", nil)
	s.WithRawWriter(bytes.NewBuffer([]byte{}))
	require.Equal(t, 1, len(s.writer))
}
