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

Set this environment variable on the custom sidecar, waypoint, or ingress gateway's
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
sending. Sidecars and ingress gateways also require istiod's
`PILOT_ENABLE_SENDING_HBONE` (enabled by default when ambient is enabled through
`PILOT_ENABLE_AMBIENT`). `ENABLE_HBONE` controls receiving HBONE on sidecars;
it is not required solely to originate HBONE with this shim.

## Generated configuration

For eligible service clusters, istiod generates all three pieces together:

- An additional `connect_originate_hbone_shim` internal listener containing the
  shim network filter. It preserves the original listener's original-destination
  filter, optional metadata filters, TCP proxy settings, and access logs.
- The paired shim transport on the waypoint's service cluster, or on the ingress
  gateway or sidecar cluster's `hbone` transport match. The existing host and cluster
  metadata passthrough is preserved.
- Internal EDS endpoints directed to that listener, retaining their endpoint
  identifiers and original-destination metadata.

The additional listener uses the original `connect_originate` upstream cluster.
Outer mTLS, certificate validation, ALPN, H2 pooling, and multiplexing continue to
come from the existing HBONE configuration. The original internal listener and
`encap` cluster remain available for other traffic. The opt-in participates in
CDS and EDS cache keys so stock and custom proxies can share one istiod.

## Scope and retries

The POC applies to EDS services on waypoints, Envoy sidecars, and ingress gateways. Effective
DestinationRule policies are checked at service, subset, and port scope.
Application TLS origination (`SIMPLE` or `MUTUAL`), PROXY protocol origination,
DNS and passthrough services, SNI passthrough, proxyless gRPC clients, and ambient
east-west gateways retain their existing behavior. Mesh `ISTIO_MUTUAL` remains supported
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

## Sidecar and ingress routing

The same custom proxyv2 binary used for waypoints supports these paths; no new
Envoy extension or proxy build registration is needed. Use a patched istiod with
the sidecar opt-in change and restart the selected proxies with the custom image
and metadata. For an injected workload or gateway, place this annotation on the
pod template (merge it with any existing proxy configuration):

```yaml
metadata:
  annotations:
    sidecar.istio.io/proxyImage: ilrudie/proxyv2:hbone-shim-poc-amd64
    proxy.istio.io/config: |
      proxyMetadata:
        ENABLE_HBONE_ORIGINATION_SHIM: "true"
```

That image must be available to the cluster; this example does not publish it.
For an explicitly managed gateway Deployment, set the `istio-proxy` image and
`ISTIO_META_...` environment variables directly if injection is not used.

Sidecars retain Istio's existing choice of destination: they originate HBONE to
ambient workload endpoints and do not acquire ingress-to-service-waypoint
routing. Ingress can call those endpoints directly, or use the existing
`istio.io/ingress-use-waypoint: "true"` service label with
`ENABLE_INGRESS_WAYPOINT_ROUTING` enabled. For ingress-to-waypoint traffic, the
CONNECT authority remains the destination service VIP:port; the outer peer is
the selected waypoint IP:15008. This is one HBONE hop at the ingress, not nested
double HBONE. CONNECT acceptance by the waypoint gates that hop; a later failure
between the waypoint and its backend still needs handling at the waypoint.

A service can mix ambient, mesh mTLS, and plaintext endpoints. Only endpoints
that would already use HBONE get the shim's internal address and `hbone`
transport. The other transport matches and endpoint metadata are preserved.
`PILOT_PREFER_SENDING_HBONE` is not required for ambient-only endpoints; the
existing preference for legacy mTLS on endpoints supporting both is unchanged.
The sidecar's internal listener uses outbound Telemetry policy, while ingress
retains the gateway policy context.

HTTP/1.1 and HTTP/2 requests retain route-level retry behavior. Raw TCP can also
use the connection-readiness gate, but has no HTTP request retry policy and gains
no replay guarantee after bytes are sent. Receiving HBONE, routing through a
service waypoint, and originating nested HBONE are separate capabilities; this
change only expands single-HBONE origination.

## Validation

Run the affected Go package tests inside the build container:

```sh
go test ./pilot/pkg/networking/core ./pilot/pkg/networking/util ./pilot/pkg/xds/endpoints ./pkg/model
go test ./pilot/pkg/xds -run TestIngressUseWaypoint
```

Tests cover opt-in defaults and scope, CDS/EDS pairing, subset and port policies,
metadata preservation, retaining the original listener, and cache isolation.
Complete outbound configuration tests cover sidecar and ingress HTTP/1.1,
HTTP/2, and TCP services with mixed HBONE/mTLS/plaintext endpoints. The ingress
waypoint test checks LDS/CDS/EDS pairing, service authority, and the selected waypoint peer.
The Envoy shim tests separately exercise CONNECT failures, safe POST retries,
H2 multiplexing, resets, and GOAWAY. A real ztunnel/mTLS deployment has not yet
been tested.
