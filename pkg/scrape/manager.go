package scrape

import (
	"github.com/pkg/errors"
	"tkestack.io/kvass/pkg/prom"
)

// Manager includes all jobs
type Manager struct {
	jobs map[string]*JobInfo
}

// New create a Manager with specified Cli set
func New() *Manager {
	return &Manager{
		jobs: map[string]*JobInfo{},
	}
}

// ApplyConfig update Manager from config
func (s *Manager) ApplyConfig(cfg *prom.ConfigInfo) error {
	ret := map[string]*JobInfo{}
	for _, cfg := range cfg.Config.ScrapeConfigs {
		info, err := newJobInfo(*cfg)
		if err != nil {
			return errors.Wrap(err, cfg.JobName)
		}
		ret[cfg.JobName] = info
	}
	s.jobs = ret
	return nil
}

// GetJob search job by job name, nil will be return if job not exist
func (s *Manager) GetJob(job string) *JobInfo {
	return s.jobs[job]
}
