# External authorization and peer identity

For HTTP requests at an ambient waypoint, a CUSTOM AuthorizationPolicy with an
`envoyExtAuthzGrpc` provider receives the authenticated caller's SPIFFE identity in
`attributes.request.http.headers["x-istio-peer-principal"]`. For example, this can
be `spiffe://cluster.local/ns/demo/sa/client`.

The waypoint terminates HBONE mTLS on `connect_terminate`, which stores the peer
identity in `io.istio.peer_principal` filter state. The HTTP authorization check
runs on `main_internal`, whose internal connection has no peer TLS certificate.
Envoy derives `attributes.source.principal` from that connection's certificate,
so that field remains empty. Enabling certificate inclusion on the authorization
provider cannot recover the original peer certificate at this layer.

For internal listeners configured to use peer filter state, Istio inserts a
header mutation immediately before each gRPC external authorization filter. It
overwrites `x-istio-peer-principal` from filter state, replacing any value supplied
by the client even if the authenticated identity is empty. An authorizer requiring
an authenticated workload must reject missing or empty identities. No EnvoyFilter
is needed for this header.

The header is also forwarded to the upstream application. Applications can trust
it only if their traffic is guaranteed to pass through the enforcing proxy.
This behavior also applies to internal gateway listeners that use authenticated
peer filter state. Direct TLS listeners such as sidecars continue to supply
`attributes.source.principal` through Envoy's normal certificate handling.
HTTP external authorization providers and TCP checks are unchanged.
