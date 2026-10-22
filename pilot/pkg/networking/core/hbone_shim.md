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
the proxy disables HBONE sending. With `AMBIENT_ENABLE_MULTI_NETWORK=true`, only
waypoints and ingress gateways with native double-HBONE support are eligible;
see the double-HBONE section below. Sidecars and ingress gateways also require istiod's
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
because HBONE supplies that mTLS. Double-HBONE origination is supported on
waypoints and ingress gateways with the native multi-network resources enabled.

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
sidecar support currently covers single-HBONE origination.

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

## Double-HBONE origination

The same shim proxy image supports nested HBONE; this expansion needs an updated
istiod, with no additional Envoy changes. Set these environment variables on istiod
in addition to the normal ambient configuration:

```yaml
env:
- name: PILOT_ENABLE_AMBIENT
  value: "true"
- name: AMBIENT_ENABLE_MULTI_NETWORK
  value: "true"
- name: AMBIENT_ENABLE_MULTI_NETWORK_WAYPOINT
  value: "true"
```

For ingress origination, also enable `AMBIENT_ENABLE_MULTI_NETWORK_INGRESS=true`
and `PILOT_ENABLE_SENDING_HBONE=true`. The source proxy must be a recognized managed
waypoint or ingress gateway, advertise `ENABLE_HBONE_ORIGINATION_SHIM`, and use the
custom proxy image. Sidecar double HBONE and ambient east-west gateway opt-in are
not supported. Leave shim metadata unset on stock E/W proxies.

Network IDs, remote service discovery, ambient E/W gateways, and trust/SDS setup
must already support native double HBONE. Enabling these flags alone does not
create a remote route. The global multi-network flag now gates native nested
resources for both proxy types, matching its documented behavior.

For eligible service clusters, inspect this chain in the source proxy's config:

```text
service cluster (shim transport)
  local endpoint  -> connect_originate_hbone_shim -> connect_originate
  remote gateway -> inner_connect_originate_hbone_shim
                 -> inner_connect_originate (H2, internal-upstream wrapping TLS)
                 -> outer_connect_originate listener (ordinary TCP CONNECT proxy)
                 -> outer_connect_originate cluster (H2, TLS)
                 -> remote E/W gateway
```

The additional inner listener preserves the native target authority from
`istio.double_hbone.hbone_target_address`, gateway address propagation, source
network header, optional baggage, and access logs. Native listeners remain present
for opt-out proxies and excluded service policies. Local and remote EDS endpoints
use the same service transport, but enter the appropriate shim listener. Remote
endpoints retain gateway identifiers, metadata, and aggregate weights. Both nested
H2 clusters retain `max_requests_per_connection: 1`.

Outer CONNECT, inner TLS, and inner H2 setup proceed before the application
connection becomes ready. Application bytes are withheld until the **inner**
CONNECT succeeds. Outer rejection, inner TLS failure, and inner CONNECT rejection
can therefore fail establishment before an application request is sent. Inner
acceptance by a remote waypoint only gates that hop; its subsequent backend hop
still needs its own handling. The application connection timeout bounds the whole
nested setup. Retries remain configured on application routes, not CONNECT routes.

The current native outer cluster overrides its port to 15008. Gateway metadata
may advertise a different port; this POC preserves the native override. Source
EDS aggregates remote pods into gateways and does not identify the selected
remote pod. Retry predicates and outlier accounting at the source consequently
operate at gateway granularity for remote traffic. Avoiding individual
terminating remote pods still requires remote-side selection behavior.

The configuration tests cover mixed local/remote endpoints, gateway weights and
metadata, listener and transport pairing, feature/capability gates, policies,
application HTTP/1.1/HTTP/2/TCP, cache separation, and TLS retention.
Live nested traffic, real identities and churn must be validated in a deployment.

## Permanent CONNECT rejections

New shim builds support an additional capability:

```yaml
proxyMetadata:
  ENABLE_HBONE_ORIGINATION_SHIM: "true"
  ENABLE_HBONE_REJECTION_HANDLING: "true"
```

Use this only with a proxy containing `envoy.upstreams.istio_hbone` and the new
nested transport support, and with Istiod built from this branch. Leave the new
flag unset on older images.

The application cluster receives a pool adapter that stops permanent CONNECT
rejections before router retry and outlier accounting. CONNECT 4xx, including
403 and 429, returns a local HTTP response with that status or closes raw TCP.
Other rejected non-5xx responses produce a local 502. CONNECT 500–599 retains
configured connection-failure retries. A successful CONNECT followed by an
application response retains the application's normal retry and health policy.

For double HBONE, the inner HTTP/2 cluster's shim transport wraps the existing TLS
socket and targets `outer_connect_originate_hbone_shim`. The outer shim gates inner
TLS until outer CONNECT acceptance and preserves rejection status through both
hops. Existing TLS identities, SNI, ALPN, listener headers and metadata remain in
place. No EDS endpoint format changes or sidecar double-HBONE expansion are needed
for this capability. Its value is included in the CDS cache key.

Access logs distinguish tunnel denials using UF and
`istio_hbone_connect_rejected:<status>` response details / upstream transport
failure reason. Connection-failure counters can increase, but permanent denials
are not reported to outlier detection. Local replies are not counted as application
responses in service-cluster upstream HTTP status counters.

This change aligns CONNECT status handling with ztunnel PR #2142's reviewed
classification. It does not add typed classification for identity acquisition,
certificate revocation or local workload-pool shutdown. Those causes still use
Envoy's existing behavior.
