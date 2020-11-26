<div align=center><img width=800 hight=400 src="./README.assets/logo.png" /></div>

[English](README.md)

Kvass 是一个 [Prometheus](https://github.com/prometheus/prometheus) 横向扩缩容解决方案，他使用Sidecar根据Coordinator分配下来的target列表来生成新的配置文件给Prometheus使用。Coordinator 用于服务发现，target分配和分片管理.
[Thanos](https://github.com/thanos-io/thanos) (或者其他TSDB) 用来将分片数据汇总成全局数据.

  [![Go Report Card](https://goreportcard.com/badge/github.com/tkestack/kvass)](https://goreportcard.com/report/github.com/tkestack/kvass)  [![Build](https://github.com/tkestack/kvass/workflows/Build/badge.svg?branch=master)]()   [![codecov](https://codecov.io/gh/tkestack/kvass/branch/master/graph/badge.svg)](https://codecov.io/gh/tkestack/kvass)

------

# 目录
   * [概览](#目录)
   * [架构](#架构)
      * [组件](#组件)
         * [Coordinator](#coordinator)
         * [Sidecar](#sidecar)
      * [Kvass + Thanos](#kvass--thanos)
      * [Kvass + 远程存储](#kvass--远程存储)
      * [多副本](#多副本)
   * [安装Demo](#安装Demo)
   * [启动参数推荐](#启动参数推荐)
   * [License](#license)


# 目录

Kvass 是一个 [Prometheus](https://github.com/prometheus/prometheus) 横向扩缩容解决方案，他有以下特点. 

* 轻量，安装方便
* 支持数千万series规模 (数千k8s节点)
* 无需修改Prometheus配置文件，无需加入hash_mod
* 分片自动扩缩容
* 根据target实际数据规模来进行分片复杂均衡，而不是用hash_mod
* 支持多副本

# 架构

<img src="./README.assets/image-20201126031456582.png" alt="image-20201126031456582" style="zoom:50%;" />

## 组件

### Coordinator

启动参数参考 [code](https://github.com/tkestack/kvass/blob/master/cmd/kvass/coordinator.go#L61)

* Coordinaotr 加载配置文件并进行服务发现，获取所有target
* 对于每个需要采集的target, Coordinator 为其应用配置文件中的"relabel_configs"，并且探测target负载
* Coordinaotr 周期性将未分配的target分配给分片，根据分片当前的Head series进行选择

<img src="./README.assets/image-20201126031409284.png" alt="image-20201126031409284" style="zoom:50%;" />

### Sidecar

启动参数参考 [code](https://github.com/tkestack/kvass/blob/master/cmd/kvass/sidecar.go#L48)

* Sidecar 从Coordinator获得target及其relabel过的Labels

* Sidecar只使用 "static_configs" 服务发现机制来生成一份新的配置文件，配置文件中只包含分配给他的target, 并删除所有"relabel_configs"

* 所有Prometheus抓取请求会被代理到Sidecar用于target规模的跟踪

  

  <img src="./README.assets/image-20201126032909776.png" alt="image-20201126032909776" style="zoom:50%;" />

## Kvass + Thanos

由于现在Prometheus的数据分散到了各个分片, 我们需要一个方案去获得全局数据。

[Thanos](https://github.com/thanos-io/thanos) 是一个不错的选择. 我们只需要将Kvass sidecar放在Thanos sidecar边上，再安装一个Kvass coordinator就可以了.

![image-20201126035103180](./README.assets/image-20201126035103180.png)

## Kvass + 远程存储

如果你使用其他远程存储例如influxdb，只需要直接在配置文件里配置 "remote write"就可以了。

## 多副本

Coordinator 使用 label 选择器来选择分片的StatefulSets, 每一个StatefulSet被认为是一个副本, Kvass将标号相同的不同.StatefulSet的Pod进行编组，同组的Prometheus将被分配相同的target，并预期有相同的负载。

> --shard.selector=app.kubernetes.io/name=prometheus

# 安装Demo

我们提供了一个demo去展示Kvass的使用.

> git clone https://github.com/tkestack/kvass
>
> cd kvass/example
>
> kubectl create -f ./examples

我可以看到一个叫"metrics"的Deployment， 其有 6 个Pod, 每个Pod会生成 10045 series (45 series 来至golang默认的metrics)。

我们将采集这些指标。

![image-20200916185943754](./README.assets/image-20200916185943754.png)

每个分片能采集的最大series在Coordinator的启动参数里配置。

在这里例子中我们设置为30000.

> ```
> --shard.max-series=30000
> ```

现在我们有6个target，总计60000+ series  每个分片最多能采30000 series，所以我们预期需要3个分片.

Coordinator自动将分片个数变成3，并将6个target分配给他们.

![image-20200916190143119](./README.assets/image-20200916190143119.png)

我们发现每个分片的head series数目确实只有2个target的量。

![image-20200917112924277](./README.assets/image-20200917112924277.png)

我们通过thanos-query查询到的，则是全部的数据。

![image-20200917112711674](./README.assets/image-20200917112711674.png)

#  启动参数推荐

Prometheus的内存使用量和head series有关。

在实际使用时，我们推荐每个分片的最大series设置成750000。

> 设置Coordinator启动参数
>
> --shard.max-series=750000

每个Prometheus的内存request建议设置为8G.

# License

Apache License 2.0, see [LICENSE](./LICENSE).

