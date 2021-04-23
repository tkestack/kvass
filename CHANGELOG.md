
## [v0.1.2] - 2021-04-23
### Fix
- empty shard need ensure idle state every coordinate period
- targets GC should include unhealthy target


## [v0.1.1] - 2021-04-21
### Feat
- use weighted random when assign new target with max-idle-timeout=0

### Fix
- scale down blocked if any idle shard exited
- scale up when target is too big
- concurrent map iteration and map write on discovery targets map
- set content-type when copy data to prometheus


## [v0.1.0] - 2021-03-09
### Feat
- external_labels not affects config hash now
- update workflow
- support min shards, change replicas management and rand assign ([#38](https://github.com/tkestack/kvass/issues/38))

### Fix
- coordinator min shard chaos with max shard
- sidecar always panic at first time started
- update workflow
- go lint
- base image


## [v0.0.6] - 2021-02-24
### Fix
- sidecar panic when scrape failed ([#32](https://github.com/tkestack/kvass/issues/32))


## [v0.0.5] - 2021-02-22
### Feat
- disable scaling down by default


## [v0.0.4] - 2021-01-18
### Fix
- scrape timeout message of targets list
- register all SD type
- register all SD type


## [v0.0.3] - 2020-12-18
### Fix
- change workflow go version to 1.15
- coordinator start with service discovery init
- statistic samples before copy data to prometheus
- remove deleted targets
- remove all auth in injected config file


## [v0.0.2] - 2020-12-11
### Fix
- Dockerfile and Makefile
- unmarshal bear_token/password of remote write/read config ([#6](https://github.com/tkestack/kvass/issues/6))
- upgrade prometheus lib
- flag descriptions of Coordinator


## v0.0.1 - 2020-11-20
### Feat
- support invalid label name
- support inject APIServer information for kubernetes SD
- coordinator support maxShard flag

### Fix
- shard client return empty RuntimeInfo if request failed

