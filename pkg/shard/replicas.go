package shard

import (
	"fmt"
	"github.com/sirupsen/logrus"
	"tkestack.io/kvass/pkg/api"
	"tkestack.io/kvass/pkg/target"
)

// Replicas is a replicas of one shard
// all replicas of one shard scrape same targets and expected to have same load
type Replicas struct {
	// ID is the unique ID for differentiate different replicate of shard
	ID string
	// scraping is a map that record the targets this Replicas scarping
	// the key is target hash, scraping cache is invalid if it is nil
	scraping map[uint64]bool
	url      string
	log      logrus.FieldLogger
}

// NewReplicas create a Replicas with empty scraping cache
func NewReplicas(id string, url string, log logrus.FieldLogger) *Replicas {
	return &Replicas{
		ID:  id,
		url: url,
		log: log,
	}
}

func (r *Replicas) targetsScraping() (map[uint64]bool, error) {
	if r.scraping == nil {
		res, err := r.targetStatus()
		if err != nil {
			return nil, err
		}
		c := map[uint64]bool{}
		for k := range res {
			c[k] = true
		}
		r.scraping = c
	}
	return r.scraping, nil
}

func (r *Replicas) runtimeInfo() (*RuntimeInfo, error) {
	res := &RuntimeInfo{}

	err := api.Get(r.url+"/api/v1/shard/runtimeinfo/", &res)
	if err != nil {
		// Shard may be unhealthy, the activeTargets scraping should be reload
		r.scraping = nil
		return nil, fmt.Errorf("get runtime info from %s failed : %s", r.ID, err.Error())
	}

	return res, nil
}

func (r *Replicas) targetStatus() (map[uint64]*target.ScrapeStatus, error) {
	res := map[uint64]*target.ScrapeStatus{}

	err := api.Get(r.url+"/api/v1/shard/targets/", &res)
	if err != nil {
		// Shard may be unhealthy, the activeTargets scraping should reload
		r.scraping = nil
		return nil, fmt.Errorf("get targets status info from %s failed : %s", r.ID, err.Error())
	}

	return res, nil
}

func (r *Replicas) updateTarget(targets map[string][]*target.Target) error {
	newCache := map[uint64]bool{}
	for _, ts := range targets {
		for _, t := range ts {
			newCache[t.Hash] = true
		}
	}

	if r.needUpdate(newCache) {
		r.log.Infof("%s need update targets", r.ID)
		if err := api.Post(r.url+"/api/v1/shard/targets/", &targets, nil); err != nil {
			return err
		}
		r.scraping = newCache
	}

	return nil
}

func (r *Replicas) needUpdate(cache map[uint64]bool) bool {
	if len(cache) != len(r.scraping) {
		return true
	}

	for k, v := range cache {
		if r.scraping[k] != v {
			return true
		}
	}
	return false
}
