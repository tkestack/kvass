module tkestack.io/kvass

go 1.17

require (
	github.com/VictoriaMetrics/VictoriaMetrics v1.71.0
	github.com/cssivision/reverseproxy v0.0.1
	github.com/gin-contrib/pprof v1.3.0
	github.com/gin-gonic/gin v1.6.3
	github.com/go-kit/kit v0.12.0
	github.com/go-kit/log v0.2.0
	github.com/gobuffalo/packr/v2 v2.2.0
	github.com/klauspost/compress v1.13.6
	github.com/mitchellh/hashstructure/v2 v2.0.1
	github.com/mroth/weightedrand v0.4.1
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.12.1
	github.com/prometheus/common v0.32.1
	github.com/prometheus/prometheus v2.28.1+incompatible
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/cobra v1.1.3
	github.com/stretchr/testify v1.7.0
	go.etcd.io/etcd v0.5.0-alpha.5.0.20200910180754-dd1b699fc489
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.22.7
	k8s.io/apimachinery v0.22.7
	k8s.io/client-go v0.22.7
)

replace github.com/prometheus/prometheus => github.com/prometheus/prometheus v0.0.0-20220324221659-44a5e705be50 // 2.35.0
