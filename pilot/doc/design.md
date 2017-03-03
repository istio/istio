# Istio Manager design overview

Istio Manager is responsible for consuming and propagating Istio configuration to Istio components. It also provides an abstraction layer over the underlying cluster management platform, such as Kubernetes, and proxy controllers for dynamic reconfiguration of Istio proxies.

## Services model

The overview of the services model in Istio Manager is [here](service-registry.md).
Istio Manager services model introduces the concept of a service version, a finer-grained service notion used to subdivide service instances by versions (`v1`, `v2`) or environment (`staging`, `prod`). Istio routing rules can refer to the service versions, to provide additional control over traffic between services.

## Configuration model

The overview of the configuration flow in Istio Manager is
[here](configuration-flow.md). The schema for specifying routing rules can
be found [here](https://github.com/istio/api/blob/master/proxy/v1/config/cfg.md).
Istio configuration is backed by a distributed key-value store. Istio Manager components subscribe to change events in the configuration store to enforce live configuration updates.

## Proxy controller

The overview of the proxy controllers in Istio Manager is [here](proxy-controller.md).
Istio Manager supervises a mesh of proxies co-located with service instances as sidecar container. A proxy agent generates fresh configuration adapted to the local proxy instances from the services and configuration models, and triggers proxy re-configuration.

![architecture](https://cdn.rawgit.com/istio/manager/master/doc/manager.svg)
