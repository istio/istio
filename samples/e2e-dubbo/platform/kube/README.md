# Xprotocol示例文档
XProtocol的定位是云原生，高性能，低侵入性的通用Service Mesh落地方案，依托Kubernetes基座，利用其原生的服务注册和服务发现机制，支持各种私有RPC协议低成本，易扩展的接入，快速享受Service Mesh所带来的红利。
本文将以Dubbo 为例，演示Dubbo on Xprotocol 场景下Service Mesh 路由功能，涵盖 Version Route , Weight Route 功能

## 前期准备
1. 已安装 kubernetes集群，集群版本1.10.0 以上
2. 了解 Istio Traffic Management 相关概念，相关链接：[https://istio.io/docs/tasks/traffic-management/](https://istio.io/docs/tasks/traffic-management/)
3. 安装部署 sofa-mesh，git clone [https://github.com/alipay/sofa-mesh.git](https://github.com/alipay/sofa-mesh.git) ， 参考istio 安装方式即可安装sofa-mesh
4. 确认kubernetes node节点已经打开iptable功能

## 部署
      先看部署效果图：

![Mosn Xprotocol部署图.png | left | 747x382](https://cdn.nlark.com/yuque/0/2018/png/151172/1536291419546-2aa160de-69cd-497f-a280-fae20a1f87a3.png "")

<span data-type="color" style="color:#F5222D">本示例中dubbo-consumer的部署方式采用直连模式，即不走注册中心，完全依托kubernetes平台提供的服务注册及服务发现能力。</span>

以下示例都将运行在e2e-dubbo 命名空间下，如无e2e-dubbo命名空间，需先创建该命名空间：
```bash
kubectl apply -f samples/e2e-dubbo/platform/kube/e2e-dubbo-ns.yaml
```

部署 dubbo-consumer 和 dubbo-provider，部署前需要先使用istioctl 进行sidecar注入，以下示例采用手动注入方式，也可以通过istio namespace inject功能来自动注入。
```bash
# mosn sidecar inject and deploy
kubectl apply -f <(istioctl kube-inject -f samples/e2e-dubbo/platform/kube/dubbo-consumer.yaml)
kubectl apply -f <(istioctl kube-inject -f samples/e2e-dubbo/platform/kube/dubbo-provider-v1.yaml)
kubectl apply -f <(istioctl kube-inject -f samples/e2e-dubbo/platform/kube/dubbo-provider-v2.yaml)
```
部署dubbo consumer service 及 dubbo provider service
```bash
# http service for dubbo consumer
kubectl apply -f samples/e2e-dubbo/platform/kube/dubbo-consumer-service.yaml

# dubbo provider service
kubectl apply -f samples/e2e-dubbo/platform/kube/dubbo-provider-service.yaml
```

检查部署状态：
```yaml
#kubectl get pods -n e2e-dubbo
NAME                                     READY     STATUS    RESTARTS   AGE
e2e-dubbo-consumer-589d8c465d-cp7cx      2/2       Running   0          13s
e2e-dubbo-provider-v1-649d7cff94-52gfd   2/2       Running   0          13s
e2e-dubbo-provider-v2-5f7d5ff648-m6c45   2/2       Running   0          13s

#kubectl get svc -n e2e-dubbo    
NAME                 TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)     AGE
e2e-dubbo-consumer   ClusterIP   192.168.1.7     <none>        8080/TCP    10s
e2e-dubbo-provider   ClusterIP   192.168.1.62    <none>        12345/TCP   10s
```

## Version Route
本例将演示控制 dubbo-consumer的所有请求指向 dubo-provider-v1
配置DestinationRule: 
```bash
istioctl create -f samples/e2e-dubbo/platform/kube/dubbo-consumer.destinationrule.yaml
```
dubbo-consumer.destinationrule.yaml 内容如下：
```yaml
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: e2e-dubbo-provider
  namespace: e2e-dubbo
spec:
  host: e2e-dubbo-provider
  subsets:
  - name: v1
    labels:
      ver: v1
  - name: v2
    labels:
      ver: v2 
```
配置VirtualService:
```bash
istioctl create -f samples/e2e-dubbo/platform/kube/dubbo-consumer.version.vs.yaml
```
dubbo-consumer.version.vs.yaml 内容如下：
```yaml
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: e2e-dubbo-provider
  namespace: e2e-dubbo
spec:
  hosts:
    - e2e-dubbo-provider
  http:
  - route:
    - destination:
        host: e2e-dubbo-provider
        subset: v1
```
路由策略已经生效，可以http请求dubbo consumer来触发rpc请求观察效果：
```bash
# 请将以下 e2e-dubbo-consume-service 替换成您集群中e2e-dubbo-consumer service的域名
curl "${e2e-dubbo-consume-service}:8080/sayHello?name=dubbo-mosn"
```
清理路由策略：
```bash
istioctl delete -f samples/e2e-dubbo/platform/kube/dubbo-consumer.destinationrule.yaml
istioctl delete -f samples/e2e-dubbo/platform/kube/dubbo-consumer.version.vs.yaml
```

## Weight Route
本例将演示控制 dubbo-consumer的请求指向 dubo-provider-v1，dubo-provider-v2。并控制流量分配比例为 v1：20%，v2：80%
配置DestinationRule: 
```bash
# 如果在上一示例中已经创建好了，请跳过这一步
istioctl create -f samples/e2e-dubbo/platform/kube/dubbo-consumer.destinationrule.yaml
```
dubbo-consumer.destinationrule.yaml内容如下：
```yaml
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: e2e-dubbo-provider
  namespace: e2e-dubbo
spec:
  host: e2e-dubbo-provider
  subsets:
  - name: v1
    labels:
      ver: v1
  - name: v2
    labels:
      ver: v2 
```
配置VirtualService:
```bash
istioctl create -f samples/e2e-dubbo/platform/kube/dubbo-consumer.weight.vs.yaml
```
dubbo-consumer.weight.vs.yaml内容如下：
```yaml
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: e2e-dubbo-provider
  namespace: e2e-dubbo
spec:
  hosts:
    - e2e-dubbo-provider
  http:
  - route:
    - destination:
        host: e2e-dubbo-provider
        subset: v1
        weight: 20
    - destination:
        host: e2e-dubbo-provider
        subset: v2
        weight: 80
```
路由策略已经生效，可以http请求dubbo consumer来触发rpc请求观察效果：
```bash
# 请将以下 e2e-dubbo-consume-service 替换成您集群中e2e-dubbo-consumer service的域名
curl "${e2e-dubbo-consume-service}:8080/sayHello?name=dubbo-mosn"
```
清理路由策略：
```bash
istioctl delete -f samples/e2e-dubbo/platform/kube/dubbo-consumer.destinationrule.yaml
istioctl delete -f samples/e2e-dubbo/platform/kube/dubbo-consumer.weight.vs.yaml
```

更多功能，敬请期待

