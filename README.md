# Ambient Mesh

This branch contains the experimental Ambient mesh functionality of Istio.
See the [Introducing Ambient Mesh](TODO) and [Getting Started with Ambient](TODO) blogs for more information.

## Getting Started

### Supported Environments

The following environments have been tested and are expected to work:

* GKE (_without_ Calico or Dataplane V2)
* EKS
* `kind`

The following environments are known to not work currently:

* GKE with Calico CNI
* GKE with Dataplane V2 CNI

All other environments are unknown currently.

If you don't have a cluster, a simple cluster can be deployed in `kind` for testing:

```shell
kindConfig="
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: ambient
nodes:
- role: control-plane
- role: worker
- role: worker
"
kind create cluster --config=- <<<"${kindConfig[@]}"
```

### Installing

Installing Istio with Ambient enabled is simple -- just pass `--set profile=ambient`.

First you will need to download the Ambient-enabled build of `istioctl`:

```shell
$ TODO
```

Then you can install like normal:

```shell
$ istioctl install -y --set profile=ambient
```

Follow [Getting Started with Ambient](TODO) for more details on getting started.

### Installing from source

```shell
HUB=my-hub # examples: localhost:5000, gcr.io/my-project
TAG=ambient
# Build the images
tools/docker --targets=pilot,proxyv2,app,install-cni --hub=$HUB --tag=$TAG --push
go run istioctl/cmd/istioctl install  --set hub=$HUB --set tag=$TAG --set profile=ambient -y
```

## Limitations

Ambient mesh is considered experimental.
As such, it is not suitable for deployment in production environments, and doesn't have the same performance, stability, compatibility, or security guidelines that Istio releases typically have.

In addition to these general caveats, there are a number of known issues in the current implementation.
These all represent short term limitations, and are expected to be fixed in future releases before promotion beyond "Experimental".

* While `AuthorizationPolicy` is generally supported, there are a number of cases where authorization checks are not as strict as expected or not applied at all.
* There are known performance issues for most measurements, including data plane throughput/latency, data plane resource consumption, scalability of cluster sizes, control plane resource consumption, and configuration propogation latency.
* There are known cases where pods may temporarily not be treated as part of the mesh throughput their lifecycle.
* Use of `NetworkPolicy` is currently undefined behavior.
* Reported telemetry data only shows `reporter=destination` metrics.
* Access log configuration is not fully respected.
* Services that are ambient-enabled are not accessible through `LoadBalancer` or `NodePort`s. However, you can deploy an ingressgateway (which is not ambient-enabled) to access services externally.
* Excessive permissions are granted to various components.
* Requests directly to pod IPs (rather than Services) do not work in some cases.
* `EnvoyFilter`s are not supported.
* Workloads not listening on `localhost` are not supported (see [here](https://istio.io/latest/blog/2021/upcoming-networking-changes/)).
* Traffic from outside the mesh does not have inbound policy applied.
* `STRICT` mTLS does not fully prevent plain text traffic.
