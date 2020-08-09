# Build patched images

## Pilot

Disables support for workload label updates, based on [upstream patch](https://github.com/istio/istio/pull/16748).

```
docker build -t sre/istio-pilot -f getyourguide/pilot.dockerfile .
docker tag sre/istio-pilot:latest 130607246975.dkr.ecr.eu-central-1.amazonaws.com/sre/istio-pilot:1.1.8-patched
docker push 130607246975.dkr.ecr.eu-central-1.amazonaws.com/sre/istio-pilot:1.1.8-patched
```

## Sidecar Injector

Puts `istio-proxy` at first position in list of containers, based on [upstream patch](https://github.com/istio/istio/pull/24737).

```
docker build -t sre/istio-sidecar-injector -f getyourguide/sidecar-injector.dockerfile .
docker tag sre/istio-sidecar-injector:latest 130607246975.dkr.ecr.eu-central-1.amazonaws.com/sre/istio-sidecar-injector:1.1.8-patched
docker push 130607246975.dkr.ecr.eu-central-1.amazonaws.com/sre/istio-sidecar-injector:1.1.8-patched
```

## Istio Proxy

Adds the `wait` command to `pilot-agent`, based on [upstream patch](https://github.com/istio/istio/pull/24737).

```
docker build -t sre/istio-proxyv2 -f getyourguide/proxyv2.dockerfile .
docker tag sre/istio-proxyv2:latest 130607246975.dkr.ecr.eu-central-1.amazonaws.com/sre/istio-proxyv2:1.1.8-patched
docker push 130607246975.dkr.ecr.eu-central-1.amazonaws.com/sre/istio-proxyv2:1.1.8-patched
```
