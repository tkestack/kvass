module tkestack.io/kvass

go 1.15

require (
	github.com/cssivision/reverseproxy v0.0.1
	github.com/fsnotify/fsnotify v1.4.9 // indirect
	github.com/gin-contrib/pprof v1.3.0
	github.com/gin-gonic/gin v1.6.3
	github.com/go-kit/kit v0.10.0
	github.com/go-kit/log v0.1.0
	github.com/gobuffalo/packr/v2 v2.2.0
	github.com/mitchellh/hashstructure/v2 v2.0.1
	github.com/mroth/weightedrand v0.4.1
	github.com/pkg/errors v0.9.1
	github.com/prometheus/common v0.29.0
	github.com/prometheus/prometheus v2.28.1+incompatible
	github.com/sirupsen/logrus v1.6.0
	github.com/spf13/cobra v1.0.0
	github.com/stretchr/testify v1.7.0
	go.etcd.io/etcd v0.0.0-20191023171146-3cf2f69b5738
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.21.1
	k8s.io/apimachinery v0.21.1
	k8s.io/client-go v0.21.1
)

replace github.com/prometheus/prometheus => github.com/prometheus/prometheus v1.8.2-0.20210701133801-b0944590a1c9 // 2.28.1
