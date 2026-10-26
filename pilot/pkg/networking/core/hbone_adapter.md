# HBONE CONNECT wait

HBONE CONNECT wait delays upstream connection readiness and application I/O until
CONNECT acceptance. Transient establishment failures can use configured
`connect-failure` retries, including for POST requests, before application data is
sent. The implementation uses the Envoy extensions in
`contrib/istio/filters/network/hbone`.

## Enable

Build istiod from this branch and use a matching proxyv2 image containing all three
extensions:

- `envoy.filters.network.istio_hbone`
- `envoy.transport_sockets.istio_hbone`
- `envoy.upstreams.istio_hbone`

The Istio proxy build has its own extension allowlist; registration in Envoy's
contrib build alone does not include these extensions in proxyv2. Retain the normal
Istio extensions, including the peer-metadata support for adapter listeners.

Set the following in the selected proxy's `ProxyConfig` and restart the proxy:

```yaml
proxyMetadata:
  ISTIO_META_ENABLE_HBONE_CONNECT_WAIT: "true"
```

For an injected workload or gateway, use the pod-template annotations below.
Replace the image placeholder with a newly built compatible image:

```yaml
metadata:
  annotations:
    sidecar.istio.io/proxyImage: example.invalid/proxyv2:hbone-connect-wait
    proxy.istio.io/config: |
      proxyMetadata:
        ISTIO_META_ENABLE_HBONE_CONNECT_WAIT: "true"
```

For an explicitly managed gateway Deployment, set the image and this environment
variable on the `istio-proxy` container directly:

```yaml
env:
- name: ISTIO_META_ENABLE_HBONE_CONNECT_WAIT
  value: "true"
```

Both paths produce `ENABLE_HBONE_CONNECT_WAIT: "true"` in Envoy's bootstrap node
metadata. Unset or false disables the feature. No EnvoyFilter is required. Stock
proxy images cannot load the extensions; use a compatible image before enabling.

Normal ambient/HBONE configuration is still required. Disabling HBONE sending on
the proxy also disables CONNECT wait. Sidecars and ingress gateways require
istiod's `PILOT_ENABLE_SENDING_HBONE`, enabled by default through
`PILOT_ENABLE_AMBIENT`. `ENABLE_HBONE` controls receiving HBONE on sidecars and is
not required solely to originate HBONE.

## Generated configuration

For eligible service clusters, istiod generates the following together:

- A `connect_originate_hbone_adapter` internal listener containing the terminal adapter
  filter, preserving the original listener's address restoration, metadata filters,
  TCP proxy settings, and access logs.
- The adapter transport on the waypoint service cluster or the sidecar/ingress
  cluster's `hbone` transport match, preserving host and cluster metadata.
- The `envoy.upstreams.istio_hbone` pool adapter on the application cluster for
  HTTP/1.1, HTTP/2, and TCP consumers, including mixed transport clusters.
- Internal EDS endpoints directed to the matching listener, preserving endpoint
  identifiers and original-destination metadata.

Single HBONE uses the existing `connect_originate` cluster for outer mTLS, identity
validation, ALPN, HTTP/2 pooling, and multiplexing. Native listeners remain
available. CDS and EDS cache keys distinguish enabled and disabled proxies.

CONNECT wait applies to EDS services. Effective DestinationRule policies are
checked at service, subset, and port scope. Application TLS origination (`SIMPLE`
or `MUTUAL`), PROXY protocol origination, DNS and passthrough services, SNI
passthrough, proxyless gRPC, and ambient east-west gateway opt-in are excluded.
Mesh `ISTIO_MUTUAL` remains supported because HBONE supplies that mTLS.

A service can mix ambient, mesh mTLS, and plaintext endpoints. Only HBONE endpoints
use the adapter listener and transport. Other transport matches retain their existing
behavior through the application pool adapter. Load-balancing policies and
ordinary HTTP/2 GOAWAY handling are unchanged; GOAWAY does not deprioritize peers.

## Routing and retries

Sidecars retain their existing choice of destination. Ingress can originate HBONE
directly to ambient endpoints or use a service waypoint through the existing
`istio.io/ingress-use-waypoint: "true"` label and
`ENABLE_INGRESS_WAYPOINT_ROUTING`. In that case the CONNECT authority remains the
service VIP:port and the outer peer is the selected waypoint IP:15008. Acceptance
by the waypoint gates that hop; the waypoint needs CONNECT wait for its backend
hop as well.

Application route retry policies are unchanged. For transient failures, configure
`connect-failure`, a nonzero retry count, and endpoint reselection, such as the
previous-host predicate. The service cluster's connection timeout includes CONNECT
establishment. Raw TCP uses the readiness gate but has no HTTP request retry policy.

CONNECT 4xx, including 403 and 429, returns a local HTTP response with that status
or closes raw TCP. Other rejected non-5xx responses produce a local 502. These
rejections bypass retries and outlier failure reporting. CONNECT 500–599 and a
CONNECT 200 with premature END_STREAM retain transient establishment-failure
behavior. Application responses inside accepted streams retain normal retry and
health policies.

Access logs identify rejected CONNECTs with `UF` and
`istio_hbone_connect_rejected:<status>` in response details and the upstream
transport failure reason. Connection-failure counters can increase, but permanent
denials are not reported to outlier detection or counted as application responses
in service-cluster upstream HTTP status counters.

After application bytes may have reached the destination, the adapter provides no
additional replay guarantee. A graceful GOAWAY prevents new CONNECT streams on
that HTTP/2 connection while accepted streams and their application connections
can continue.

## Double HBONE

In addition to normal ambient configuration, enable these istiod settings for
waypoint double HBONE:

```yaml
env:
- name: PILOT_ENABLE_AMBIENT
  value: "true"
- name: AMBIENT_ENABLE_MULTI_NETWORK
  value: "true"
- name: AMBIENT_ENABLE_MULTI_NETWORK_WAYPOINT
  value: "true"
```

For ingress, also enable `AMBIENT_ENABLE_MULTI_NETWORK_INGRESS=true` and
`PILOT_ENABLE_SENDING_HBONE=true`. The source must be a recognized managed waypoint
or ingress gateway. Network IDs, remote discovery, E/W gateways, trust, and SDS must
already support native double HBONE. Sidecar double HBONE is unsupported. With
multi-network enabled, proxies lacking native double-HBONE resources remain on
their existing path.

The same CONNECT wait option configures both hops:

```text
application cluster (adapter transport and pool adapter)
  local endpoint -> connect_originate_hbone_adapter -> connect_originate
  remote gateway -> inner_connect_originate_hbone_adapter
                 -> inner_connect_originate (H2, adapter transport wrapping TLS)
                 -> outer_connect_originate_hbone_adapter
                 -> outer_connect_originate (H2, TLS)
                 -> remote E/W gateway
```

The outer adapter gates inner TLS until outer CONNECT acceptance. The application
transport waits for inner CONNECT acceptance. Existing TLS identity checks,
headers, optional baggage, metadata, and access logs are retained. Both nested H2
clusters retain `max_requests_per_connection: 1`; the pool adapter is installed
on application clusters only.

Remote endpoints preserve gateway identifiers, metadata, and aggregate weights.
The native outer cluster still overrides its port to 15008. Source EDS aggregates
remote pods into gateways, so source-side retry predicates and outlier accounting
operate at gateway granularity. Avoiding individual terminating remote pods still
requires remote-side selection behavior.

## Validation

Run the focused configuration and metadata tests inside the build container:

```sh
go test ./pilot/pkg/networking/core ./pilot/pkg/networking/util ./pilot/pkg/xds/endpoints ./pilot/pkg/xds ./pkg/model ./pkg/bootstrap -run 'TestHBONE|TestIngressUseWaypoint|TestNodeMetadata'
```

Tests cover enablement and metadata parsing, CDS/LDS/EDS pairing, mixed transports,
service/subset/port policies, cache isolation, and nested TLS retention. Envoy's
adapter tests separately exercise accepted CONNECTs, failures, POST retries,
multiplexing, and GOAWAY. Live interoperability with Istio 1.31-style ztunnel remains
to be validated for this branch.
