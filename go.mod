module tkestack.io/kvass

go 1.14

require (
	github.com/gin-contrib/pprof v1.3.0
	github.com/gin-gonic/gin v1.6.3
	github.com/go-kit/kit v0.10.0
	github.com/golang/protobuf v1.4.2 // indirect
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.5.1
	github.com/prometheus/common v0.9.1
	github.com/prometheus/prometheus v0.0.0-00010101000000-000000000000
	github.com/sirupsen/logrus v1.4.2
	github.com/spf13/cobra v1.0.0
	github.com/stretchr/testify v1.5.1
	go.etcd.io/etcd v0.0.0-20191023171146-3cf2f69b5738
	golang.org/x/sync v0.0.0-20200317015054-43a5402ce75a
	google.golang.org/grpc v1.32.0 // indirect
	gopkg.in/yaml.v2 v2.2.8
	gopkg.in/yaml.v3 v3.0.0-20200313102051-9f266ea9e77c
	k8s.io/api v0.17.5
	k8s.io/apimachinery v0.17.5
	k8s.io/client-go v0.17.5
)

replace github.com/prometheus/prometheus => github.com/prometheus/prometheus v0.0.0-20200507164740-ecee9c8abfd1
