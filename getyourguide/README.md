# Build patched images

## Pilot

1.  Disables support for workload label updates, based on [upstream patch](https://github.com/istio/istio/pull/16748).
2.  Filters out STATIC service entry endpoints with `network` field from Ingress Gateways, based on [upstream patch](https://github.com/istio/istio/pull/26729/files). Context for the problem can be found in [this doc](https://docs.google.com/document/d/19Bp-rL4GSZfuwKZKJSt9VPPOAwfnW1MrWyO_mvmBjXE/edit#heading=h.jepy8uc455ut).

    -   `REQUESTED_NETWORK_VIEW` needs to be set in the Ingress Gateway to `networkA`.
    -   `network` needs to be set to the ServiceEntry (with resolution type `STATIC`) to `networkB`.
    -   [meshNetworks](https://github.com/getyourguide/k8s-platform/blob/master/charts/istio/values.jinja2.yaml#L629) needs to be set.

    ```yaml
    meshNetworks:
      networkB:
        endpoints:
        - fromCidr: "10.20.0.0/20"
        gateways:
        - address: 10.20.7.196
            port: 8001
    ```

    -   All proxies without `REQUESTED_NETWORK_VIEW` set will see all networks. Proxies with `REQUESTED_NETWORK_VIEW` configured will only see the local network plus the ones explicitly defined.

```shell
docker build -t 130607246975.dkr.ecr.eu-central-1.amazonaws.com/sre/istio-pilot:1.1.8-patched -f getyourguide/pilot.dockerfile .
docker push 130607246975.dkr.ecr.eu-central-1.amazonaws.com/sre/istio-pilot:1.1.8-patched
```

## Sidecar Injector

Puts `istio-proxy` at first position in list of containers, based on [upstream patch](https://github.com/istio/istio/pull/24737).

```shell
docker build -t sre/istio-sidecar-injector -f getyourguide/sidecar-injector.dockerfile .
docker tag sre/istio-sidecar-injector:latest 130607246975.dkr.ecr.eu-central-1.amazonaws.com/sre/istio-sidecar-injector:1.1.8-patched
docker push 130607246975.dkr.ecr.eu-central-1.amazonaws.com/sre/istio-sidecar-injector:1.1.8-patched
```

## Istio Proxy

Adds the `wait` command to `pilot-agent`, based on [upstream patch](https://github.com/istio/istio/pull/24737).

```shell
docker build -t sre/istio-proxyv2 -f getyourguide/proxyv2.dockerfile .
docker tag sre/istio-proxyv2:latest 130607246975.dkr.ecr.eu-central-1.amazonaws.com/sre/istio-proxyv2:1.1.8-patched
docker push 130607246975.dkr.ecr.eu-central-1.amazonaws.com/sre/istio-proxyv2:1.1.8-patched
```
