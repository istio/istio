# Notes

Initial build takes a really long time, maybe we should have them do some steps to get that rolling up front?


## Prep Steps

1. launch a codespace from your fork
1. checkout a feature branch from master, call it what you'd like. Perhaps `contribfest`
    ```shell
    export TAG=istio-testing
    export HUB=localhost:5000
    export DOCKER_TARGETS='docker.pilot docker.proxyv2 docker.ztunnel docker.install-cni'
    export DOCKER_BUILD_VARIANTS="distroless"
    ```
1. `./prow/integ-suite-kind.sh --skip-cleanup`

## Install Istio

1. `make -B $(pwd)/out/linux_amd64/release/istioctl-linux-amd64`
1. `alias istioctl="$(pwd)/out/linux_amd64/release/istioctl-linux-amd64"`
1. `istioctl install --set profile=ambient --skip-confirmation --set tag=istio-testing --set hub=localhost:5000 --set values.global.imagePullPolicy=Always`
1. maybe talk about delayed informers... probably not
1. `kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.0/standard-install.yaml`

## Setup and test a basic playground

```shell
kubectl create namespace server
kubectl label namespace server istio.io/dataplane-mode=ambient
kubectl apply -f https://raw.githubusercontent.com/istio/istio/refs/heads/master/samples/httpbin/httpbin.yaml -n server
kubectl label -n server svc httpbin istio.io/use-waypoint=waypoint
kubectl create namespace client
kubectl label namespace client istio.io/dataplane-mode=ambient
kubectl apply -f https://raw.githubusercontent.com/istio/istio/refs/heads/master/samples/sleep/sleep.yaml -n client
```

check the environment

```shell
kubectl exec -n client deploy/sleep -it -- curl -v http://httpbin.server:8000/headers
kubectl logs -n istio-system ds/ztunnel | grep outbound | tail -1
```

(optionally) observer the ztunnel config
`istioctl zc all -o yaml | less`

## Observe the happy path

```shell
istioctl waypoint apply -n server
istioctl waypoint status -n server
# success!
```

## Recreate the bug

```shell
# reset
istioctl waypoint delete -n server waypoint
# scale down istiod so nothing programs waypoints
kubectl scale deploy/istiod -n istio-system --replicas=0
# try again
istioctl waypoint apply -n server
# this time we don't wait for a long time though, we know it's not going to be programmed by anything
istioctl waypoint status -n server --waypoint-timeout=10s
# observe the problem
```

## A reasonable development loop

```shell
# change istioctl somehow
# hint, look in istioctl/pkg/waypoint/waypoint.go
# build istioctl
make -B $(pwd)/out/linux_amd64/release/istioctl-linux-amd64
# test istioctl
istioctl waypoint status -n server --waypoint-timeout=10s --my-fixed-flag=my-flag-setting
```