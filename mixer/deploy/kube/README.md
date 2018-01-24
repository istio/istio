# Istio Mixer K8s Configuration

These configurations stand up an instance of Mixer, a Prometheus instance that scrapes metrics Mixer exposes, and
a Grafana instance to render those metrics. These are intended for development and local testing, not a real production
deployment. The easiest way to stand up these deployments is to run (from Mixer's root directory):
```shell
$ kubectl create configmap prometheus-config --from-file=testdata/prometheus.yaml
$ kubectl create configmap mixer-config --from-file=testdata/globalconfig.yml --from-file=testdata/serviceconfig.yml
$ kubectl apply -f ./deploy/kube/
```
    
If you're using something like [minikube](https://github.com/kubernetes/minikube) to test locally, you can call the
mixer server with our test client (`mixc`), providing some attribute values:

```shell
$ MIXS=$(minikube service mixer --url --format "{{.IP}}:{{.Port}}" | head -n 1)
$ bazel run //cmd/client:mixc -- report "something happened" -m $MIXS -a source.name=source,target.name=target,api.name=myapi,api.method=v1.foo.bar,response.http.code=200,response.duration=100,client.id=$USER
```

<aside class="notice">
Note: due to [Issue #272](https://github.com/istio/mixer/issues/272) a `Check` or `Report` call must be made to Mixer
before Prometheus will be able to scrape metrics.
</aside>

## mixer.yaml
`mixer.yaml` describes a deployment and service for Mixer's server binary. Two pieces of configuration are required to
run Mixer: a global configuration and a service configuration. Example configurations can be found in the `//testdata`
directory (`//` indicates the [root of Mixer's project directory](https://github.com/istio/mixer)). These
configurations are expected to be mounted in two files at `/etc/opt/mixer/`. We use a configmap named `mixer-config` to
provide these configurations. Usually this configmap is created from the `//testdata/` directory by:

<a name="configmap_command"></a>
```shell
$ kubectl create configmap mixer-config --from-file=testdata/globalconfig.yml --from-file=testdata/serviceconfig.yml
```
A file can be created to capture this configmap by running:

```shell
$ kubectl get configmap mixer-config -o yaml > ./mixer-config.yaml
```
We do not check in this file because we consider the yaml files in `//testdata` the source of truth for this config.

## grafana.yaml
`grafana.yaml` describes a deployment and service for Grafana, based on the [`grafana/grafana` docker image](https://hub.docker.com/r/grafana/grafana/).
The Grafana UI is exposed on port `3000`. This configuration stores Grafana's data in the `/data/grafana` dir, which we
configure to be an ephemeral directory. To persist your Grafana configuration for longer than the lifetime of the `grafana`
pod, map this directory to a real volume. Note that it is possible to export your Grafana config and re-import it later.

An example grafana dashboard is provided in `conf/grafana-dashboard.json`. It can
be imported into a running grafana instance that has a `prometheus` datasource
configured at `http://prometheus:9090/`.


If you're using minikube to test these deployments locally, get to the UI by running:

```shell
$ minikube service grafana
```

We also need to add our Prometheus installation data source. If you're using the provided `prometheus.yaml`, the data
source is at `http://prometheus:9090/` (no auth required, access via proxy).

## <a name="prometheus"></a> prometheus.yaml
`prometheus.yaml` describes a deployment and service for a Prometheus collection server. An example configuration that
works with these deployments is stored in `//testdata/`. Prometheus expects its config to be mounted at
`/etc/opt/mixer/prometheus.yaml`. We use a configmap named `prometheus-config` to provide these configurations. Usually
this configmap is created from the `//deploy/kube/conf` directory by:
```shell
$ kubectl create configmap prometheus-config --from-file=prometheus.yaml
```

The Prometheus configuration at `conf/prometheus.yaml` looks for a Kubernetes service named `mixer` with a port
named `prometheus` to scrape. The Kubernetes service defined in `mixer.yaml` satisfies these requirements.

#### Configuring Mixer to produce Prometheus metrics
Mixer configuration in the `//testdata` directory is set up to run the prometheus adapter already. To insert it
into your own mixer deployment the adapter needs to be registered in the global config:

```yaml
adapters:
  - name: prometheus
    kind: metrics
    impl: prometheus
    params: # the prometheus adapter doesn't take any parameters; see //adapter/prometheus/config/config.proto
```
         
Then the metrics aspect must be configured so that the adapter can populate metric values from attributes at runtime. This
is part of the service config:
```yaml
aspects:
  - kind: metrics
    adapter: prometheus # needs to match the name of the prometheus metrics adapter in the global config
    params:
      metrics:
      - descriptor: request_count
        # we want to increment this counter by 1 for each unique (source, target, service, method, response_code) tuple
        value: "1"
        labels:
          source: source.name
          target: target.name
          service: api.name | "unknown"
          method: api.method | "unknown"
          response_code: response.http.code
      - descriptor:  request_latency
        value: response.duration | "0ms"
        labels:
          source: source.name
          target: target.name
          service: api.name | "unknown"
          method: api.method | "unknown"
```

## OPTIONAL: Running a local registry for development
`localregistry.yaml` is a copy of Kubernete's local registry addon, and is included to make it easier to test
Mixer by allowing a developer to push docker images locally rather than to some remote registry. Run the
registry (`kubectl apply -f ./deploy/kube/localregistry.yaml`), and update `mixer.yaml` to use the image
`localhost:5000/mixer`. After the registry server is running, expose it locally by executing:

```shell
$ kubectl port-forward --namespace kube-system $POD 5000:5000
```

If you're testing locally with minikube, `$POD` can be set with:

```shell
$ POD=$(kubectl get pods --namespace kube-system -l k8s-app=kube-registry \
  -o template --template '{{range .items}}{{.metadata.name}} {{.status.phase}}{{"\n"}}{{end}}' \
  | grep Running | head -1 | cut -f1 -d' ')
```

Then you can build Mixer, create a docker image, and push it to the local registry, by running:
```shell
$ bazel build ...:all
$ bazel run //docker:mixer localhost:5000/mixer
$ docker push localhost:5000/mixer
```
