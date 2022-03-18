package scrape

import (
	"github.com/sirupsen/logrus"
	"tkestack.io/kvass/pkg/prom"
)

// Manager includes all jobs
type Manager struct {
	jobs            map[string]*JobInfo
	keeAliveDisable bool
	lg              logrus.FieldLogger
}

// New create a Manager with specified Cli set
func New(keeAliveDisable bool, lg logrus.FieldLogger) *Manager {
	return &Manager{
		lg:              lg,
		keeAliveDisable: keeAliveDisable,
		jobs:            map[string]*JobInfo{},
	}
}

// ApplyConfig update Manager from config
func (s *Manager) ApplyConfig(cfg *prom.ConfigInfo) error {
	ret := map[string]*JobInfo{}
	for _, cfg := range cfg.Config.ScrapeConfigs {
		info, err := newJobInfo(*cfg, s.keeAliveDisable)
		if err != nil {
			s.lg.Error(err.Error())
			continue
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
