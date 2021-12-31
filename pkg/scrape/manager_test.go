package scrape

import (
	"net/url"
	"os"
	"testing"
	"time"

	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"tkestack.io/kvass/pkg/prom"
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
	defer os.Unsetenv("SCRAPE_PROXY")

	ss := New(logrus.New())
	r.NoError(ss.ApplyConfig(&prom.ConfigInfo{
		Config: &config.Config{
			ScrapeConfigs: []*config.ScrapeConfig{cfg},
		},
	}))
	s := ss.GetJob(cfg.JobName)
	r.NotNil(s)

	r.Equal(u.String(), s.proxyURL.String())
}
