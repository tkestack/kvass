package scrape

import (
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/protoparser/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/relabel"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestStatisticSample(t *testing.T) {
	total := StatisticSeries([]prometheus.Row{
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
	})
	require.Equal(t, int64(1), total)
}
