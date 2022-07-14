package discovery

import (
	"fmt"
	"hash/fnv"
	"net"
	"net/url"
	"sort"
	"strings"
	"tkestack.io/kvass/pkg/target"

	"github.com/prometheus/prometheus/model/relabel"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/labels"

	"tkestack.io/kvass/pkg/utils/types"

	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/scrape"
)

// addPort checks whether we should add a default port to the address.
// If the address is not valid, we don't append a port either.
func addPort(s string) bool {
	// If we can split, a port exists and we don'target have to add one.
	if _, _, err := net.SplitHostPort(s); err == nil {
		return false
	}
	// If adding a port makes it valid, the previous error
	// was not due to an invalid address and we can append a port.
	_, _, err := net.SplitHostPort(s + ":1234")
	return err == nil
}

func completePort(addr string, scheme string) (string, error) {
	// If it's an address with no trailing port, infer it based on the used scheme.
	if addPort(addr) {
		// Addresses reaching this point are already wrapped in [] if necessary.
		switch scheme {
		case "http", "":
			addr = addr + ":80"
		case "https":
			addr = addr + ":443"
		default:
			return "", errors.Errorf("invalid scheme: %q", scheme)
		}
	}
	return addr, nil
}

// populateLabels builds a label set from the given label set and scrape configuration.
// It returns a label set before relabeling was applied as the second return value.
// Returns the original discovered label set found before relabelling was applied if the target is dropped during relabeling.
func populateLabels(lset labels.Labels, cfg *config.ScrapeConfig) (res, orig labels.Labels, err error) {
	lb := labels.NewBuilder(lset)
	// Copy labels into the labelset for the target if they are not set already.
	scrapeLabels := []labels.Label{
		{Name: model.JobLabel, Value: cfg.JobName},
		{Name: model.MetricsPathLabel, Value: cfg.MetricsPath},
		{Name: model.SchemeLabel, Value: cfg.Scheme},
	}

	for _, l := range scrapeLabels {
		if lv := lset.Get(l.Name); lv == "" {
			lb.Set(l.Name, l.Value)
		}
	}
	// Encode scrape query parameters as labels.
	for k, v := range cfg.Params {
		if len(v) > 0 {
			lb.Set(model.ParamLabelPrefix+k, v[0])
		}
	}

	preRelabelLabels := lb.Labels()
	lset = relabel.Process(preRelabelLabels, cfg.RelabelConfigs...)

	// Get if the target was dropped.
	if lset == nil {
		return nil, preRelabelLabels, nil
	}
	if v := lset.Get(model.AddressLabel); v == "" {
		return nil, nil, errors.New("no address")
	}

	lb = labels.NewBuilder(lset)
	addr, err := completePort(lset.Get(model.AddressLabel), lset.Get(model.SchemeLabel))
	if err != nil {
		return nil, nil, err
	}
	lb.Set(model.AddressLabel, addr)

	if err := config.CheckTargetAddress(model.LabelValue(addr)); err != nil {
		return nil, nil, err
	}

	// Meta labels are deleted after relabelling. Other internal labels propagate to
	// the target which decides whether they will be part of their label set.
	for _, l := range lset {
		if strings.HasPrefix(l.Name, model.MetaLabelPrefix) {
			lb.Del(l.Name)
		}
	}

	// Default the instance label to the target address.
	if v := lset.Get(model.InstanceLabel); v == "" {
		lb.Set(model.InstanceLabel, addr)
	}

	res = lb.Labels()
	for _, l := range res {
		// Get label values are valid, drop the target if not.
		if !model.LabelValue(l.Value).IsValid() {
			return nil, nil, errors.Errorf("invalid label value for %q: %q", l.Name, l.Value)
		}
	}
	return res, preRelabelLabels, nil
}

// targetsFromGroup builds activeTargets based on the given TargetGroup and config.
func targetsFromGroup(tg *targetgroup.Group, cfg *config.ScrapeConfig) ([]*SDTargets, error) {
	targets := make([]*SDTargets, 0, len(tg.Targets))
	exists := map[uint64]bool{}

	for i, tlset := range tg.Targets {
		lbls := make([]labels.Label, 0, len(tlset)+len(tg.Labels))

		for ln, lv := range tlset {
			lbls = append(lbls, labels.Label{Name: string(ln), Value: string(lv)})
		}
		for ln, lv := range tg.Labels {
			if _, ok := tlset[ln]; !ok {
				lbls = append(lbls, labels.Label{Name: string(ln), Value: string(lv)})
			}
		}

		lset := labels.New(lbls...)

		lbls, origLabels, err := populateLabels(lset, cfg)
		if err != nil {
			return nil, errors.Wrapf(err, "instance %d in group %s", i, tg)
		}

		if lbls != nil || origLabels != nil {
			tar := scrape.NewTarget(lbls, origLabels, cfg.Params)
			hash := targetHash(lbls, tar.URL().String())
			if exists[hash] {
				continue
			}
			exists[hash] = true
			targets = append(targets, &SDTargets{
				Job:        cfg.JobName,
				PromTarget: tar,
				ShardTarget: &target.Target{
					Hash:   hash,
					Labels: supportInvalidLabelName(labelsWithoutConfigParam(lbls, cfg.Params)),
				},
			})
		}
	}
	return targets, nil
}

func targetHash(lbls labels.Labels, url string) uint64 {
	h := fnv.New64a()
	//nolint: errcheck
	sort.Sort(lbls)
	_, _ = h.Write([]byte(fmt.Sprintf("%016d", lbls.Hash())))
	//nolint: errcheck
	_, _ = h.Write([]byte(url))
	return h.Sum64()
}

// some config param is not valid as label values
// but populateLabels will add all config param into labels
// must delete them from label set
func labelsWithoutConfigParam(lbls labels.Labels, param url.Values) labels.Labels {
	key := make([]string, 0, len(param))
	for k := range param {
		key = append(key, model.ParamLabelPrefix+k)
	}

	newlbls := labels.Labels{}
	for _, l := range lbls {
		if !types.FindString(l.Name, key...) {
			newlbls = append(newlbls, l)
		}
	}
	return newlbls
}

// some label's name is invalid but we do need it
// add prefix string to valid it
// sidecar should add relabel_configs to remove prefix
func supportInvalidLabelName(lbls labels.Labels) labels.Labels {
	res := labels.Labels{}
	for _, l := range lbls {
		if !model.LabelName(l.Name).IsValid() {
			l.Name = target.PrefixForInvalidLabelName + l.Name
		}
		res = append(res, l)
	}
	return res
}
