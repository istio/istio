# Istio Pilot design overview

Istio Pilot is responsible for consuming and propagating Istio configuration to Istio components. It also provides an abstraction layer over the underlying cluster management platform, such as Kubernetes, and proxy controllers for dynamic reconfiguration of Istio proxies.

## Services model

The overview of the services model in Istio Pilot is [here](service-registry.md).
Istio Pilot services model introduces the concept of a service version, a finer-grained service notion used to subdivide service instances by versions (`v1`, `v2`) or environment (`staging`, `prod`). Istio routing rules can refer to the service versions, to provide additional control over traffic between services.

## Configuration model

The overview of the configuration flow in Istio Pilot is
[here](configuration-flow.md). The schema for specifying routing rules can
be found [here](https://github.com/istio/api/blob/master/proxy/v1/config).
Istio configuration is backed by a distributed key-value store. Istio Pilot components subscribe to change events in the configuration store to enforce live configuration updates.

## Proxy controller

The overview of the proxy controllers in Istio Pilot is [here](proxy-controller.md).
Istio Pilot supervises a mesh of proxies co-located with service instances as sidecar container. A proxy agent generates fresh configuration adapted to the local proxy instances from the services and configuration models, and triggers proxy re-configuration.

![architecture](https://cdn.rawgit.com/istio/pilot/master/doc/pilot.svg)

The diagram uses _black_ arrows for the data path and _red_ arrows for the control path. Proxies capture traffic from services and route them internally and externally using control information from the discovery services and agent-generated configurations. This control information is stored in the API server in Kubernetes and is managed by the operator using `kubectl` or `istioctl`.
