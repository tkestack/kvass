package scrape

import (
	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/stretchr/testify/require"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestManager(t *testing.T) {
	r := require.New(t)
	cfg := &config.ScrapeConfig{
		JobName: "test",
	}
	u, _ := url.Parse("http://127.0.0.1:8008")
	cfg.HTTPClientConfig.ProxyURL = config_util.URL{URL: u}
	cfg.ScrapeTimeout = model.Duration(time.Second)
	r.NoError(os.Setenv("SCRAPE_PROXY", "http://127.0.0.1:9090"))

	ss := New()
	r.NoError(ss.ApplyConfig(&config.Config{ScrapeConfigs: []*config.ScrapeConfig{cfg}}))
	s := ss.GetJob(cfg.JobName)
	r.NotNil(s)

	r.Equal(time.Second, s.timeout)
	r.Equal(u.String(), s.proxyURL.String())
}
