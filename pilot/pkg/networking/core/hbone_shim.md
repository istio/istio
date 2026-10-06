# Experimental HBONE origination shim

This POC configures the paired Envoy extensions in
`contrib/istio/filters/network/hbone`. A CONNECT rejection becomes an upstream
connection failure before the application router sends its request, allowing
`connect-failure` retries, including for POST requests.

## Enable

Build istiod with this change and use a custom Istio proxy binary containing both
`envoy.filters.network.istio_hbone` and
`envoy.transport_sockets.istio_hbone`. The Istio proxy build has its own extension
allowlist; registering the extensions in Envoy's contrib build alone does not
include them in proxyv2. The image must also retain the normal Istio extensions.

Set this environment variable on the custom waypoint or ingress gateway's
`istio-proxy` container, then restart that proxy:

```yaml
env:
- name: ISTIO_META_ENABLE_HBONE_ORIGINATION_SHIM
  value: "true"
```

This supplies the bootstrap node metadata
`ENABLE_HBONE_ORIGINATION_SHIM: "true"`. Leave it unset on stock images. A stock
binary cannot load the new extensions. No EnvoyFilter is required.

Normal ambient/HBONE configuration is still required. The POC is disabled when
istiod's `AMBIENT_ENABLE_MULTI_NETWORK` is true or the proxy disables HBONE
sending. Ingress gateways also require `PILOT_ENABLE_SENDING_HBONE`.

## Generated configuration

For eligible service clusters, istiod generates all three pieces together:

- An additional `connect_originate_hbone_shim` internal listener containing the
  shim network filter. It preserves the original listener's original-destination
  filter, optional metadata filters, TCP proxy settings, and access logs.
- The paired shim transport on the waypoint's service cluster, or on the ingress
  gateway cluster's `hbone` transport match. The existing host and cluster
  metadata passthrough is preserved.
- Internal EDS endpoints directed to that listener, retaining their endpoint
  identifiers and original-destination metadata.

The additional listener uses the original `connect_originate` upstream cluster.
Outer mTLS, certificate validation, ALPN, H2 pooling, and multiplexing continue to
come from the existing HBONE configuration. The original internal listener and
`encap` cluster remain available for other traffic. The opt-in participates in
CDS and EDS cache keys so stock and custom proxies can share one istiod.

## Scope and retries

The POC applies to EDS services on waypoints and ingress gateways. Effective
DestinationRule policies are checked at service, subset, and port scope.
Application TLS origination (`SIMPLE` or `MUTUAL`), PROXY protocol origination,
DNS and passthrough services, SNI passthrough, sidecars, and ambient east-west
gateways retain their existing behavior. Mesh `ISTIO_MUTUAL` remains supported
because HBONE supplies that mTLS on the outer connection. Double HBONE is outside
this POC.

Route retry policies are unchanged. Confirm the application route includes
`connect-failure`, has a nonzero retry count, and can select another endpoint
(for example using the previous-host predicate). Existing application requests
and ambiguous failures after readiness are not made safe to retry by this shim.
A graceful outer GOAWAY leaves accepted CONNECT streams alive.

The service cluster's connection timeout now includes CONNECT establishment.
Keep it long enough for the outer connection and application TCP connection
attempt, while bounding the time a request waits.

## Validation

Run the affected Go package tests inside the build container:

```sh
go test ./pilot/pkg/networking/core ./pilot/pkg/networking/util ./pilot/pkg/xds/endpoints ./pkg/model
```

Tests cover opt-in defaults and scope, CDS/EDS pairing, subset and port policies,
metadata preservation, retaining the original listener, and cache isolation.
The Envoy shim tests separately exercise CONNECT failures, safe POST retries,
H2 multiplexing, resets, and GOAWAY. A real ztunnel/mTLS deployment has not yet
been tested.
