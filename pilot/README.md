# Istio Pilot

Istio Pilot provides platform-independent service discovery, and exposes an
interface to configure rich L7 routing features such as label based routing
across multiple service versions, fault injection, timeouts, retries,
circuit breakers. It translates these configurations into sidecar-specific
configuration and dynamically reconfigures the sidecars in the service mesh
data plane. Platform-specific eccentricities are abstracted and a
simplified service discovery interface is presented to the sidecars based
on the Envoy data plane API.

Please see
[Istio's traffic management concepts](https://istio.io/docs/concepts/traffic-management/overview.html)
to learn more about the design of Pilot and the capabilities it provides.

Istio Pilot [design](doc/design.md) gives an architectural overview of its
internal components - cluster platform abstractions, service model, and the
proxy controllers.

