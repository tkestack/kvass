<div align=center><img width=800 hight=400 src="./README.assets/logo.png" /></div>

[English](README.md)

Kvass 是一个 [Prometheus](https://github.com/prometheus/prometheus) 横向扩缩容解决方案，他使用Sidecar动态得根据Coordinator分配下来的target列表来为每个Prometheus生成只含特定target的配置文件，从而将采集任务动态调度到各个Prometheus分片。
Coordinator 用于服务发现，target调度和分片扩缩容管理.
[Thanos](https://github.com/thanos-io/thanos) (或者其他TSDB) 用来将分片数据汇总成全局数据.

  [![Go Report Card](https://goreportcard.com/badge/github.com/tkestack/kvass)](https://goreportcard.com/report/github.com/tkestack/kvass)  [![Build](https://github.com/tkestack/kvass/workflows/Build/badge.svg?branch=master)]()   [![codecov](https://codecov.io/gh/tkestack/kvass/branch/master/graph/badge.svg)](https://codecov.io/gh/tkestack/kvass)

------

# 目录
   * [概述](#概述)
   * [设计](#设计)
        * [核心架构](#核心架构)
      * [组件](#组件)
        * [Coordinator](#coordinator)
        * [Sidecar](#sidecar)
      * [Kvass + Thanos](#kvass--thanos)
      * [Kvass + 远程存储](#kvass--远程存储)
      * [多副本](#多副本)
      * [Target迁移原理](#Target迁移原理)
      * [分片降压](#分片降压)
      * [分片缩容](#分片缩容)
      * [限制分片数目](#限制分片数目)
      * [Target调度策略](#Target调度策略)
   * [Demo体验](#Demo体验)
   * [最佳实践](#最佳实践)
         * [启动参数推荐](#启动参数推荐)
   * [License](#license)

# 概述

Kvass 是一个 [Prometheus](https://github.com/prometheus/prometheus) 横向扩缩容解决方案，他有以下特点. 

* 轻量，安装方便
* 支持数千万series规模 (数千k8s节点)
* 无需修改Prometheus配置文件，无需加入hash_mod
* target动态调度
* 根据target实际数据规模来进行分片复杂均衡，而不是用hash_mod
* 支持多副本

# 设计

## 核心架构

<img src="./README.assets/image-20201126031456582.png" alt="image-20201126031456582" style="zoom:50%;" />

## 组件

### Coordinator

启动参数参考 [code](https://github.com/tkestack/kvass/blob/master/cmd/kvass/coordinator.go#L61)

* Coordinaotr 加载配置文件并进行服务发现，获取所有target
* 对于每个需要采集的target, Coordinator 为其应用配置文件中的"relabel_configs"，并且探测target负载
* Coordinaotr 周期性分配新Target，转移Target，以及进行分片的缩容。

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

Coordinator 使用 label 选择器来选择分片的StatefulSets, 每一个StatefulSet被认为是一个副本, 副本之间的target分配与调度是独立的。

> --shard.selector=app.kubernetes.io/name=prometheus

## Target迁移原理

在某些场景下我们需要将一个已分配的Target从一个分片转移到另外一个分片（例如为分片降压）。

为了保证数据不断点，Target迁移被分为以下几个步骤。

* 将原在所在分片中该Target的状态标记为in_transfer，并将Target同时分配给目标分片，状态为normal。
* 等待Target被2个分片同时采集至少3个周期。
* 将原来分片中的Target删除。

## 分片降压

当一个Target分片给一个分片后，随着时间推移，Target产品的series有可能增加，从而导致分片的head series超过阈值，例如新加入的k8s节点，其cadvisor数据规模就有可能随着Pod被调度上来而增加。

当分片head series超过阈值一定比例后，Coordinator会尝试做分片的降压处理，即根据超过阈值的比例，将一部分Target从该分片转移到其他空闲分片中，超过阈值比例越高，被转移的Target就越多。

## 分片缩容

分片缩容只会从标号最大的分片开始。

当编号最大的分片上所有Target都可以被迁移到其他分片，就会尝试进行迁移，即清空编号最大的分片。

当分片被清空后，分片会变为闲置状态，经过一段时间后（等待分片数据被删除或者被上传至对象存储），分片被删除。

您可以通过Coordinaor的以下参数来设置闲置时间，当设置为0时关闭缩容。

> ```
> --shard.max-idle-time=3h 
> --shard.max-idle-time=0 // 默认
> ```

如果使用的是Statefulset来管理分片，您可以添加一下参数来让Coordinator在删除分片时自动删除pvc

> ```
> --shard.delete-pvc=true // 默认
> ```


## 限制分片数目
可通过设置以下参数来限制Coordinator的最大最小分片数。
值得注意的是，如果设置了最小分片数，那么只有可用分片数不低于最小分片数才会开始Coordinate。

> ``` 
> --shard.max-shard=99999 //默认
> --shard.min-shard=0 //默认
> ```

## Target调度策略

如果开启了缩容，那么无论是新的Target还是被迁移的Target，都会优先被分配给编号低的分片。

如果关闭了缩容，则会随机分配到有空间的分片上，这种方式特别适合和```--shard.min-shard```参数一起使用。

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

# 最佳实践

## 启动参数推荐

Prometheus的内存使用量和head series有关。

在实际使用时，我们推荐每个分片的最大series设置成750000。

> 设置Coordinator启动参数
>
> --shard.max-series=600000

每个Prometheus的内存request建议设置为2C8G.

# License

Apache License 2.0, see [LICENSE](./LICENSE).

