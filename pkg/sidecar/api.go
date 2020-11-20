package sidecar

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"tkestack.io/kvass/pkg/shard"
	"tkestack.io/kvass/pkg/target"

	"tkestack.io/kvass/pkg/utils/types"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/prometheus/config"
	"github.com/sirupsen/logrus"
	"tkestack.io/kvass/pkg/api"
	"tkestack.io/kvass/pkg/prom"
)

// API is the api server of shard
type API struct {
	*gin.Engine
	ConfigReload chan *config.Config
	TargetReload chan map[string][]*target.Target
	readConfig   func() ([]byte, error)
	promURL      string
	promCli      *prom.Client
	proxy        *Proxy
	lg           logrus.FieldLogger
	paths        []string
}

// NewAPI create new api server of shard
func NewAPI(
	promURL string,
	proxy *Proxy,
	readConfig func() ([]byte, error),
	lg logrus.FieldLogger) *API {
	w := &API{
		ConfigReload: make(chan *config.Config, 2),
		TargetReload: make(chan map[string][]*target.Target, 2),
		Engine:       gin.Default(),
		lg:           lg,
		promURL:      promURL,
		promCli:      prom.NewClient(promURL),
		proxy:        proxy,
	}

	w.GET(w.path("/api/v1/shard/runtimeinfo/"), api.Wrap(w.lg, w.runtimeInfo))
	w.GET(w.path("/api/v1/shard/targets/"), api.Wrap(w.lg, w.getTargets))
	w.POST(w.path("/api/v1/shard/targets/"), api.Wrap(w.lg, w.updateTargets))
	w.GET(w.path("/api/v1/status/config/"), api.Wrap(lg, func(ctx *gin.Context) *api.Result {
		return prom.APIReadConfig(readConfig)
	}))
	w.POST(w.path("/-/reload/"), api.Wrap(lg, func(ctx *gin.Context) *api.Result {
		return prom.APIReloadConfig(readConfig, w.ConfigReload)
	}))
	return w
}

func (w *API) path(p string) string {
	w.paths = append(w.paths, p)
	return p
}

func (w *API) ServeHTTP(wt http.ResponseWriter, r *http.Request) {
	if types.FindStringVague(r.URL.Path, w.paths...) {
		w.Engine.ServeHTTP(wt, r)
		return
	}

	u, _ := url.Parse(w.promURL)
	httputil.NewSingleHostReverseProxy(u).ServeHTTP(wt, r)
}

// Run start API at "address"
func (w *API) Run(address string) error {
	return http.ListenAndServe(address, w)
}

func (w *API) runtimeInfo(g *gin.Context) *api.Result {
	r, err := w.promCli.RuntimeInfo()
	if err != nil {
		return api.InternalErr(err, "get runtime from prometheus")
	}

	min := int64(0)
	for _, r := range w.proxy.targets {
		min += r.Series
	}

	if r.TimeSeriesCount < min {
		r.TimeSeriesCount = min
	}
	return api.Data(&shard.RuntimeInfo{HeadSeries: r.TimeSeriesCount})
}

func (w *API) getTargets(g *gin.Context) *api.Result {
	return api.Data(w.proxy.targets)
}

func (w *API) updateTargets(g *gin.Context) *api.Result {
	m := map[string][]*target.Target{}
	if err := g.BindJSON(&m); err != nil {
		return api.BadDataErr(err, "bind json")
	}
	w.TargetReload <- m
	return api.Data(nil)
}
