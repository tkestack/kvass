package shard

import (
	"fmt"
	"sync"
	"time"
	"tkestack.io/kvass/pkg/target"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// RuntimeInfo contains all running status of this shard
type RuntimeInfo struct {
	// HeadSeries return current head_series of prometheus
	HeadSeries int64 `json:"headSeries"`
	// IdleAt is the time this shard become idle
	// it is nil if this shard is not idle
	// TODO: user IdleAt to support shard scaling down
	IdleAt *time.Time `json:"idleAt,omitempty"`
}

// Shard is a shard instance contains one or more replicates
// it knows how to communicate with shard sidecar and manager local scraping cache
type Shard struct {
	ID         string
	log        logrus.FieldLogger
	replicates []*Replicas
}

// NewShard return a new shard with no replicate
func NewShard(id string, lg logrus.FieldLogger) *Shard {
	return &Shard{
		ID:         id,
		log:        lg,
		replicates: []*Replicas{},
	}
}

// AddReplicas add a Replicas to this shard
func (s *Shard) AddReplicas(r *Replicas) {
	s.replicates = append(s.replicates, r)
}

// Replicas return all replicates of this shard
func (s *Shard) Replicas() []*Replicas {
	return s.replicates
}

// targetsScraping return the targets hash that this Shard scraping
// the key of the map is target hash
// the result is union set of all replicates
func (s *Shard) TargetsScraping() (map[uint64]bool, error) {
	ret := map[uint64]bool{}
	l := sync.Mutex{}
	err := s.shardsDo(func(sd *Replicas) error {
		res, err := sd.targetsScraping()
		if err != nil {
			return err
		}

		l.Lock()
		defer l.Unlock()

		for k := range res {
			ret[k] = true
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return ret, nil
}

// RuntimeInfo return the runtime status of this Shard
func (s *Shard) RuntimeInfo() (*RuntimeInfo, error) {
	ret := &RuntimeInfo{}
	l := sync.Mutex{}
	err := s.shardsDo(func(sd *Replicas) error {
		r, err := sd.runtimeInfo()
		if err != nil {
			return err
		}

		l.Lock()
		defer l.Unlock()

		if ret.HeadSeries < r.HeadSeries {
			ret = r
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ret, nil
}

// TargetStatus return the target runtime status that Shard scraping
func (s *Shard) TargetStatus() (map[uint64]*target.ScrapeStatus, error) {
	ret := map[uint64]*target.ScrapeStatus{}
	l := sync.Mutex{}
	err := s.shardsDo(func(sd *Replicas) error {
		res, err := sd.targetStatus()
		if err != nil {
			return err
		}
		l.Lock()
		defer l.Unlock()

		for hash, t := range res {
			ret[hash] = t
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ret, nil
}

// UpdateTarget update the scraping targets of this Shard
// every Replicas will compare the new targets to it's targets scraping cache and decide if communicate with sidecar or not,
// request will be send to sidecar only if new activeTargets not eq to the scraping
func (s *Shard) UpdateTarget(targets map[string][]*target.Target) error {
	return s.shardsDo(func(sd *Replicas) error {
		return sd.updateTarget(targets)
	})
}

func (s *Shard) shardsDo(f func(sd *Replicas) error) error {
	g := errgroup.Group{}
	success := false
	for _, tsd := range s.replicates {
		sd := tsd
		g.Go(func() error {
			if err := f(sd); err != nil {
				s.log.Error(err.Error(), sd.ID)
			} else {
				success = true
			}
			return nil
		})
	}
	_ = g.Wait()
	if !success {
		return fmt.Errorf("no success shard in group %s", s.ID)
	}

	return nil
}
