# Gateway API Backend implementation

## Status

Implementation design for experimental Gateway API `XBackend` support. The
first implementation targets the `gateway.networking.x-k8s.io/v1alpha1`
`XBackend` API shipped by Gateway API v1.6.0. That API currently supports only
`ExternalHostname`, but the internal model deliberately leaves endpoint
selection and connection policy extensible.

This document is also the implementation log. Each stage records decisions,
open questions, and validation results as the work proceeds.

## Problem

Gateway API Backend is a consumer-side description of how a Gateway connects
to a destination. It must not create a generally addressable frontend merely
because the Backend object exists. Runtime configuration must be activated by
an accepted Route reference.

Converting every XBackend into a ServiceEntry is therefore the wrong semantic
boundary:

* ServiceEntry registers the external hostname as a mesh service even when no
  Route references the Backend.
* Multiple Backends targeting one hostname but using different TLS, protocol,
  or future connection settings collide on one service identity.
* ServiceEntry contains frontend, visibility, address allocation, workload
  selection, and ambient behavior that a route-local backend does not need.
* Future Backend fields would be constrained by the ServiceEntry and
  DestinationRule APIs instead of compiling into the most appropriate Pilot
  model.

InferencePool demonstrates the cost of treating a custom backend as a Service:
it needs a shadow Kubernetes Service, labels carrying extension metadata, and
special cases in route conversion. Backend should introduce a reusable
first-class backend compilation boundary instead.

## Goals

* Activate XBackend runtime state only when an accepted Route references it.
* Give each Backend an opaque service identity independent of the external
  hostname.
* Allow two Backends for the same hostname to have independent connection
  configuration.
* Reuse Pilot's service, endpoint, cluster, and TLS machinery below a small
  adapter rather than synthesizing Kubernetes or Istio API resources.
* Make the route-to-backend-to-Gateway graph the shared source for translation
  and XBackend status.
* Establish an extensible internal representation for EndpointSelector and
  additional connection policy in later Gateway API releases.

## Non-goals for the first implementation

* Implement API fields not present in Gateway API v1.6.0.
* Make XBackend directly addressable by mesh applications.
* Replace the general ServiceEntry registry.
* Refactor every existing custom backend in the first change. InferencePool can
  migrate to the new boundary separately once it has proven stable.
* Support XBackend from mesh-parent Routes until its visibility and client
  identity semantics are explicitly designed. Initial activation is for Routes
  attached to managed Gateways.

## Architecture

### 1. Route reference graph

Route collections already extract `AncestorBackend` records from backendRefs.
Extend this extraction to recognize XBackend and retain the source Route and
attached managed Gateway. A raw reference is not sufficient for activation:
the binding must be derived after parent attachment and ReferenceGrant checks.

The normalized activation key is:

```text
XBackend UID + consuming Gateway namespace/name
```

The implementation may merge bindings with identical effective configuration
later, but the source model remains consumer-aware. Keeping Gateway identity in
the key leaves room for per-consumer credentials, policy, and status.

### 2. BackendBinding intermediate representation

`BackendBinding` is a comparable krt object owned by the Gateway controller.
It is not an Istio config resource.

Initial fields:

```go
type BackendBinding struct {
    Source          TypedNamespacedName
    SourceUID       types.UID
    Gateway         types.NamespacedName
    InternalName    host.Name
    Namespace       string
    Port            model.Port
    Endpoint        BackendEndpoint
    Protocol        gatewayx.BackendProtocol
    TLS             *gatewayx.BackendTLS
    CreationTime    time.Time
}

type BackendEndpoint struct {
    Kind     BackendEndpointKind
    Hostname string
}
```

The opaque hostname must be deterministic, DNS-safe, and collision resistant.
The first form is:

```text
<xbackend-name>.<namespace>.<gateway-name>.<gateway-namespace>.xbackend.internal
```

UID remains part of object identity and equality so delete/recreate cannot
silently retain stale state. If the DNS name exceeds limits, components are
truncated with a stable hash suffix.

### 3. Runtime service adapter

A dedicated Gateway backend registry consumes active BackendBindings and
implements the existing external service-registry interface. For each binding
it creates:

* one `model.Service` using `InternalName`, the Backend namespace, the declared
  port, DNS resolution, and mesh-external attributes;
* one DNS `model.ServiceInstance` whose endpoint address is the real external
  hostname and whose endpoint port is `spec.port.port`.

This is intentionally an adapter below BackendBinding, not a generated
ServiceEntry. It does not participate in ServiceEntry address allocation,
workload selection, hostname registration, or frontend generation. Bootstrap
adds this registry to the aggregate service controller when Gateway API alpha
features are enabled.

The adapter owns XDS service/endpoint update notifications. It exposes only
active bindings, so deleting the last accepted Route reference removes the
service and cluster.

### 4. Route translation

`buildDestination` gains an XBackend case. It fetches the referenced object,
validates ReferenceGrant for cross-namespace references, and resolves the
binding for the current consuming Gateway. The generated VirtualService
destination uses `BackendBinding.InternalName` and the XBackend port.

Gateway API v1.6.0 makes the port part of XBackend spec. If backendRef.port is
also set, the initial rule is that it must equal `spec.port.port`; disagreement
sets `ResolvedRefs=False`/`BackendNotFound`-equivalent route status rather than
silently choosing one.

Route conversion currently runs once for the gateway result even when a Route
has several parents. Because bindings are keyed by Gateway, destination
materialization must occur per parent or use a parent-independent internal name
until conversion is split. The first implementation should prefer splitting
gateway conversion by parent; using a parent-independent name is an acceptable
temporary decision only if it is recorded here and covered by a migration TODO.

### 5. Connection policy

BackendBinding owns normalized connection settings. For the first increment:

* `protocol` controls the model service port protocol and, where necessary,
  generated traffic policy.
* inline TLS compiles to a generated DestinationRule for the opaque hostname,
  reusing BackendTLSPolicy validation and credential helpers where semantics
  match.
* `ServerOnly` maps to SIMPLE TLS, `ClientAndServer` maps to MUTUAL TLS, and
  `None` disables TLS.
* validation hostname defaults to the external hostname; certificate refs and
  CA refs are checked with ReferenceGrant rules and existing credential naming.

DestinationRule is an output adapter, not the Backend IR. New Backend fields
may compile directly into cluster or route metadata when DestinationRule is not
an exact semantic fit.

### 6. Status

XBackend status is derived from the same accepted binding graph. Istio writes
one ancestor entry per managed Gateway and preserves entries written by other
controllers.

Initial conditions:

* `Accepted=True` when at least one accepted Route for the Gateway produces a
  valid binding.
* `Accepted=False` for a referenced Backend whose type, endpoint, port,
  protocol, TLS configuration, or references cannot be compiled.
* an unreferenced XBackend has no Istio ancestor entry and no runtime state.

Status merging must replace only entries whose controller name is Istio's,
following the existing multi-controller-safe Policy and Route status patterns.

## Staged implementation plan and log

### Stage 0: schema and informer plumbing

Scope:

* Add XBackend to schema metadata and generated GVK/GVR/kind/collection files.
* Add the v1.6.0 experimental CRD minimum-version requirement.
* Watch XBackend only under `PILOT_ENABLE_ALPHA_GATEWAY_API`.
* Add XBackend to reference lookup and status registration.

Decisions:

* Use the upstream `apisx/v1alpha1.XBackend` type; do not copy or reinterpret
  the proposed GEP schema.
* Gate all behavior with the existing alpha Gateway API feature flag.
* Model XBackend as its own schema resource using upstream `BackendSpec` and
  `BackendStatus`; the Go resource is named XBackend while its spec/status
  types deliberately omit the experimental `X` prefix upstream.
* Require Gateway API bundle version v1.6.0 for the experimental XBackend CRD,
  matching the first upstream release that ships the type.

Open questions:

* The shared CRD version filter treats an absent bundle-version annotation as
  acceptable and only rejects a present version below the minimum. Is that
  compatibility behavior sufficient for all experimental CRD install paths?
* The Pilot integration fixture at
  `tests/integration/pilot/testdata/gateway-api-crd.yaml` already contains the
  v1.6.0 experimental XBackend CRD. Broader test suites may use other install
  paths, so later end-to-end coverage must confirm its target environment has
  experimental CRDs enabled.

Validation:

* `go generate ./pkg/config/schema` regenerated GVK, GVR, kind, collection,
  kube client/type, and CRD client adapters successfully.
* `go test ./pkg/config/schema/... ./pilot/pkg/config/kube/crdclient` passed.
* `git diff --check` passed.
* Controller integration watches XBackend only when
  `PILOT_ENABLE_ALPHA_GATEWAY_API` is enabled and otherwise supplies an empty
  static collection.

### Stage 1: BackendBinding graph

Scope:

* Define BackendBinding and deterministic internal naming.
* Index Routes by XBackend and join references with attached Gateways.
* Enforce namespace and ReferenceGrant rules before activation.
* Expose active and invalid binding results for route translation and status.

Decisions:

* A Backend object alone never creates a binding.
* Managed Gateway attachment is part of activation.
* Preserve Gateway identity in the IR even if runtime services initially
  deduplicate.
* Activation is the intersection of `AncestorBackend` edges and the existing
  accepted `RouteAttachment` output. A declared `parentRef` by itself is not
  sufficient to produce either a binding or an Istio ancestor status edge.
* Keep one binding edge per XBackend/consuming Gateway, but derive
  `InternalName` only from XBackend namespace/name and a hash containing its
  UID. The v1.6.0 API has no per-consumer override, so all current edges can
  share one runtime service while retaining Gateway identity for status and
  future policy.
* Preserve one `BackendBindingResult` per attached Route reference. This gives
  route translation/status access to missing ReferenceGrant and compilation
  failures without creating an active runtime binding. The active collection
  collapses valid results to one XBackend/Gateway edge.
* Normalize the declared Backend protocol into the model port at compilation
  time, while retaining the Gateway API protocol and TLS objects in the IR for
  later policy compilation.

Open questions:

* The existing `AncestorBackend` graph is based on declared parents rather than
  fully accepted attachments. Can it be safely enriched, or should a new graph
  be built from `RouteAttachment` output?
* How should one Route with multiple managed Gateway parents affect route
  conversion and internal names?
* Should mirror references activate bindings in the first increment?
* When a future Backend field can produce different effective configuration
  per Gateway, route conversion must split by parent and `InternalName` must
  gain an effective-configuration or consumer component. Until then, the
  parent-independent name avoids duplicate identical clusters.
* Missing XBackend objects are still diagnosed by route translation because a
  collection driven by existing XBackend objects cannot emit a result for a
  missing object. If status needs a single graph for this case, add a second
  result collection driven by route backend references.
* The first increment activates ordinary rule `backendRefs`; HTTP mirror
  filter references are not represented by `AncestorBackend` and therefore do
  not activate a binding.

Validation:

* Added focused tests proving that a declared parent does not activate an
  XBackend without an accepted managed-Gateway `RouteAttachment`, and that the
  accepted attachment does activate exactly one consumer-aware binding.
* Added compilation tests for endpoint/port/protocol normalization,
  deterministic opaque naming, endpoint-hostname isolation, and UID-sensitive
  delete/recreate identity.
* `go test ./pilot/pkg/config/kube/gateway -run
  'Test(CompileBackendBinding|BackendBindingsRequireAcceptedAttachment)$'`
  passes.

### Stage 2: dedicated runtime registry

Scope:

* Convert active ExternalHostname bindings to model services and DNS instances.
* Add the registry to aggregate discovery during bootstrap.
* Send precise service, endpoint, and full-push notifications.
* Verify removal after the last Route reference disappears.

Decisions:

* Do not publish ServiceEntry configs.
* Do not register the external hostname as a service hostname.
* Keep the adapter small enough that EndpointSelector can replace only the
  endpoint-source half later.
* The registry input is a narrow `RuntimeBackend` projection rather than the
  complete `BackendBinding`; Gateway compilation remains the owner of the IR.
* Binding edges remain consumer-specific, but the v1.6 runtime projection
  deduplicates edges by opaque `InternalName`. The service persists until the
  last edge for that name is removed. Inputs are sorted by edge key before
  projection so a future inconsistent duplicate cannot make output depend on
  map iteration order.
* Use a dedicated `GatewayBackend` registry provider ID. Runtime services use
  DNS resolution, an unspecified frontend address, and the opaque hostname;
  the real external hostname appears only as the DNS endpoint address.
* DNS endpoint changes send EDS updates and a full service push. DNS clusters
  embed endpoint hostnames in CDS, so an EDS-only update is insufficient.

Open questions:

* Should `RuntimeBackend` eventually move beside `BackendBinding`, with the
  registry consuming an interface, or remain the adapter-owned bootstrap
  contract? Keeping it adapter-owned currently avoids importing Gateway API
  types into service discovery.
* Should a conflicting pair of edges with one `InternalName` fail closed? The
  current v1.6 compiler guarantees equal effective configuration and the
  registry deterministically selects the lowest edge key as defense in depth.
* Does ambient discovery need these services, or is classic gateway xDS
  sufficient for the first increment?

Validation:

* `go test -race ./pilot/pkg/serviceregistry/gatewaybackend`
* Tests prove that adding a second Gateway edge does not duplicate or update
  the runtime service, removing one edge retains it, and removing the final
  edge emits deletion.
* Tests prove that the external FQDN is never registered as a service hostname
  and that two opaque Backend identities may independently target one FQDN.
* Bootstrap now adds the Gateway Backend registry to aggregate discovery only
  with alpha Gateway API enabled. The aggregate controller owns its lifecycle
  and synchronization with the other service registries.

### Stage 3: route and policy compilation

Scope:

* Resolve XBackend backendRefs in HTTPRoute, GRPCRoute, TCPRoute, TLSRoute, and
  request-mirror paths where the upstream API permits them.
* Point generated routes at the opaque binding hostname.
* Compile protocol and inline TLS into connection policy.
* Report invalid references through Route status.

Decisions:

* XBackend spec port is authoritative; an explicit route port must match.
* Inline Backend TLS is independent from BackendTLSPolicy, which does not target
  XBackend.
* TLS SNI uses `validation.hostname` when configured and otherwise falls back
  to the external hostname; it never uses the opaque service name.
* Policy compilation exposes three collections: per-binding results for
  diagnostics, a fail-closed valid-binding projection for runtime publication,
  and DestinationRules deduplicated by opaque hostname.
* `protocol: MCP` is rejected during BackendBinding compilation. Mapping it to
  TCP would accept a protocol contract Istio cannot implement.
* `ServerOnly` TLS compiles to SIMPLE TLS and reuses BackendTLSPolicy CA
  resolution. `ClientAndServer` currently fails closed: DestinationRule has one
  credential name, while XBackend independently identifies the client identity
  and CA validation source, so choosing either credential would discard required
  semantics.

Open questions:

* Can existing BackendTLSPolicy helpers be factored without coupling XBackend
  to policy attachment semantics?
* How should `protocol: MCP` behave in Envoy-based Istio, which has no native
  MCP backend primitive equivalent to agentgateway?
* Should unsupported protocols reject the entire Backend or only affected
  Routes?
* Supporting `ClientAndServer` requires an xDS-level policy adapter capable of
  independently sourcing client identity and validation context, or an Istio API
  extension that represents both references.

Validation:

* Focused tests cover ServerOnly TLS on the opaque host, validation-hostname
  SNI, explicit MCP rejection, and fail-closed mutual TLS behavior.
* Route translation resolves XBackend references to the opaque hostname for
  every supported BackendRef-bearing Route conversion path, validates an
  optional route port against the authoritative XBackend port, rejects mesh
  parents for this increment, and runs the same fail-closed policy validation
  used by runtime publication.
* Missing XBackends and invalid connection policy are therefore reported by
  Route translation instead of producing accepted routes to absent clusters.

### Stage 4: status, lifecycle, and debt follow-up

Scope:

* Write and merge XBackend ancestor status.
* Cover add, update, delete, ReferenceGrant, and last-reference removal.
* Document whether InferencePool can migrate from shadow Services to
  BackendBinding endpoint plugins.

Decisions:

* Status and runtime activation come from one binding result; they must not
  independently rediscover references.
* Preserve other controllers' ancestor entries.
* Emit one Istio-owned ancestor per consuming Gateway, replace all stale
  Istio-owned entries, and retain other controllers' entries byte-for-byte.
* Use `Accepted` with reasons `Accepted` and `Invalid`; v1.6.0 defines the
  condition type but does not publish typed reason constants.
* An unreferenced XBackend emits no Istio ancestor. A policy compilation failure
  changes the corresponding Gateway ancestor to `Accepted=False`.

Open questions:

* Upstream condition reasons for XBackend are still experimental; which reason
  strings are conformance-significant in v1.6.0?
* Is migration of InferencePool appropriate in this change or a follow-up?
* The generic status writer's `GetStatus` type switch includes XBackend.

Validation:

* Focused tests prove other-controller preservation, stale Istio-entry pruning,
  deterministic Gateway ancestor construction, observed-generation updates,
  and policy-error propagation to `Accepted=False`.
* Combined integration tests passed for Gateway translation, the new registry,
  aggregate discovery, bootstrap, schema generation consumers, and the CRD
  client.
* `PILOT_ENABLE_ALPHA_GATEWAY_API=true go test
  ./pilot/pkg/config/kube/gateway ./pilot/pkg/bootstrap -count=1` passed,
  exercising the real XBackend informer and bootstrap branch.
* `go test ./pilot/pkg/...` and the gateway-backend registry race test passed.

## Required test matrix

* Unreferenced XBackend creates no service, endpoint, cluster policy, or Istio
  ancestor status.
* A valid same-namespace HTTPRoute reference activates one binding.
* Removing the last reference removes the binding and runtime service.
* Two Routes referencing one Backend do not create duplicate runtime services
  for the same consumer Gateway.
* Two Gateways referencing one Backend retain distinct consumer bindings.
* Two Backends targeting one FQDN retain distinct internal identities and TLS
  settings.
* Cross-namespace references fail without and succeed with ReferenceGrant.
* Missing Backend, invalid port, unsupported protocol, invalid CA ref, and
  invalid client certificate ref update Route and Backend status correctly.
* HTTP, HTTP2, HTTP11, H2C, TCP, GRPC, and MCP protocols either map explicitly
  or fail explicitly; none silently degrade.
* ServerOnly, ClientAndServer, and None TLS modes generate the expected cluster
  policy.
* Status updates preserve entries from other controllers.
* Existing Service, ServiceEntry Hostname, ServiceImport, InferencePool, and
  BackendTLSPolicy tests remain unchanged.

## Completion criteria

The experiment is end-to-end when a referenced ExternalHostname XBackend
produces an accepted binding, an opaque DNS service and endpoint, a translated
route and connection policy, and ancestor status; deleting its final accepted
reference removes all runtime state. The implementation must demonstrate that
an unreferenced XBackend and the real external hostname do not become frontends.

## Istio-wide convergence pass

### Recommendation and naming

`BackendBinding` should become the single compiled representation of an
**outbound destination**, but it should not replace `model.Service` or the
workload API `Service`. Those objects also describe addressable frontends,
ownership, policy attachment, DNS/VIP capture, and workload membership. Rename
the generalized IR to `DestinationBinding` to avoid both Gateway ownership and
the overloaded word "Backend". Gateway API `BackendBinding` becomes one source
adapter that emits `DestinationBinding` edges.

The important separation is:

```text
source objects             consumer references       compiled runtime
Kubernetes Service ----\                            /-> classic Service view
ServiceEntry -----------+-> DestinationDefinition --+-> endpoint resolver
XBackend ---------------+       + ReferenceEdge      +-> ambient destination view
InferencePool ----------/       = DestinationBinding +-> route/cluster policy

FrontendDefinition (VIP, DNS name, listener/capture, policy attachment)
is separate and is emitted only by sources that actually declare a frontend.
```

A Kubernetes Service normally emits both a frontend and a reusable destination
definition. A ServiceEntry may emit both, subject to its hosts, addresses,
resolution, location, and visibility. XBackend emits only a destination
definition and requires a reference edge. InferencePool emits a destination
definition plus extension-processing metadata, but should not need a shadow
Kubernetes Service. This is the debt boundary the current implementation has
exposed.

### Competing models today

The current code has four overlapping representations:

* `model.Service` and `model.IstioEndpoint` in `pilot/pkg/model/service.go` are
  the classic sidecar/gateway currency. `PushContext.ServiceIndex`,
  `SidecarScope`, `ServiceForHostname`, and `GatewayServices` select these by
  hostname and visibility. CDS then iterates `model.Service` in
  `pilot/pkg/networking/core/cluster.go`, applies DestinationRule in
  `cluster_traffic_policy.go`, and obtains endpoints through
  `pilot/pkg/xds/endpoints/endpoint_builder.go`.
* Ambient independently compiles Kubernetes Service and ServiceEntry into
  `model.ServiceInfo` containing workload-api `Service` in
  `pilot/pkg/serviceregistry/ambient/services.go`. `ambientindex.go` publishes
  these to ztunnel and exposes `ServicesForWaypoint`; waypoint listeners and
  clusters convert them back to `model.Service` in
  `pilot/pkg/networking/core/waypoint.go` and `listener_waypoint.go`.
* The XBackend implementation has Gateway-owned `BackendBinding`, an adapter
  `RuntimeBackend`, and a dedicated classic `ServiceDiscovery` controller in
  `pilot/pkg/serviceregistry/gatewaybackend/controller.go`. It is not an input
  to the ambient collections, so the same route can have a cluster in a classic
  ingress gateway but no coherent ambient/waypoint destination representation.
* InferencePool uses a Kubernetes shadow Service reconciler in
  `pilot/pkg/config/kube/gateway/controller.go`, labels on `model.Service`
  (`UseInferenceSemantics`), route `Extra` metadata merging in
  `route_collections.go`, and special cluster/filter behavior. The shadow
  frontend exists to enter a service-shaped pipeline, not because the API
  declares a frontend.

Kubernetes `ExternalName` demonstrates a smaller version of the same
conflation: `pilot/pkg/serviceregistry/kube/conversion.go` preserves the
Kubernetes service hostname as the frontend while storing the real DNS target
in `ServiceAttributes.ExternalName`. ServiceEntry conversion in
`pilot/pkg/serviceregistry/serviceentry/conversion.go` combines frontend hosts,
addresses, workload selection, resolution, and endpoints in one conversion.
Those source-specific semantics must be preserved, but their downstream
destination projection can be shared.

### Proposed core types and ownership

Place the source-neutral types under `pilot/pkg/model/destination` (or
`pilot/pkg/model` if package-cycle analysis rules out the subpackage), not under
Gateway translation or a service registry:

```go
type DefinitionID struct {
    Source model.ConfigKey       // stable kind/namespace/name
    UID    string                // delete/recreate boundary when available
    Port   string                // source port identity
}

type DestinationDefinition struct {
    ID         DefinitionID
    Namespace  string
    Ports      []DestinationPort
    Endpoints  EndpointSource
    Connection ConnectionPolicy
    Metadata   DestinationMetadata
}

type ReferenceEdge struct {
    Consumer    ConsumerID       // proxy scope, Gateway, waypoint, or mesh
    Referencer  model.ConfigKey  // Route/VirtualService/Sidecar/etc.
    Destination DefinitionID
    Port        string
    Grant       AuthorizationResult
}

type DestinationBinding struct {
    Key          BindingKey      // definition + effective policy + consumer scope
    RuntimeName  host.Name       // opaque when there is no declared frontend
    Definition   DefinitionID
    Consumer     ConsumerID
    Port         DestinationPort
    Endpoints    EndpointSource
    Connection   ConnectionPolicy
    Visibility   ConsumerSet
    Dependencies sets.Set[model.ConfigKey]
}
```

Use a closed, comparable endpoint-source descriptor rather than a Go interface
inside krt objects. The first variants should be `ServiceMembership`
(Kubernetes/ServiceEntry selector), `StaticEndpoints`, `DNS`, `DynamicDNS`, and
`ExtensionResolved` (InferencePool). Resolver plugins consume those descriptors
and emit a common `DestinationEndpoint` collection. Endpoint plugins own
source-specific watches and health/locality/network metadata; the core compiler
owns identity, authorization, dependency tracking, and deduplication.

`ConnectionPolicy` is normalized cluster intent: protocol, TLS identity and
validation sources, SNI, transport socket mode, load balancing, session
persistence, retry/connection limits, and extension metadata. It is not a
DestinationRule protobuf. DestinationRule, BackendTLSPolicy, XBackend inline
TLS, and future policies are inputs to one precedence/attachment compiler.

`FrontendDefinition` remains separate and contains declared hostnames,
VIPs/addresses, capture mode, inbound ports, export/visibility, waypoint
attachment, service identity, and policy-attachment identity. It may point to a
default `DestinationDefinition`, but a destination never implicitly creates a
frontend.

### Activation, visibility, and security

Definitions and bindings have different lifecycles:

* Definitions may exist eagerly so references can be validated and status can
  be computed. They produce no xDS alone.
* A declared frontend activates the binding needed to serve that frontend.
  Kubernetes Service and normally ServiceEntry use this path.
* A valid reference edge activates a consumer-scoped binding. XBackend and
  InferencePool use this path. The last edge removal deactivates it.
* Mesh-wide destinations referenced by VirtualService or Sidecar are scoped to
  the proxies that can actually observe the referencer and definition. Do not
  translate "active somewhere" into an aggregate-registry public service.

Visibility must be a property of the binding's allowed consumer set, computed
from all applicable constraints: source export rules, ServiceEntry visibility,
route attachment, ReferenceGrant, Sidecar egress hosts, namespace tenancy, and
ambient waypoint ownership. `PushContext.serviceExportTo` and
`IsServiceVisible` remain the compatibility authority during migration; raw
`ExportTo` must not be copied into the new IR. A reference cannot broaden the
definition's visibility, and an opaque runtime name must not make a destination
discoverable to unrelated proxies.

ReferenceGrant authorizes the object reference, not traffic identity. TLS
credential reads need their existing independent authorization. Authorization
Policy continues to attach to frontends/workloads, while connection identity
and credential material attach to the destination binding. The design must
explicitly decide whether an egress destination without a frontend is a valid
AuthorizationPolicy target before exposing such attachment.

### Classic, ambient, and waypoint projections

Create one `DestinationIndex` containing active bindings and resolved
endpoints. It exposes consumer-filtered snapshots and precise dependency keys.
Both dataplane paths consume it:

* The classic adapter projects bindings into `model.Service` only as a
  compatibility cluster key and projects resolved endpoints into
  `EndpointIndex`. This replaces the XBackend-specific registry first and, over
  time, the destination half of Kubernetes and ServiceEntry registries.
  `SidecarScope` and `GatewayServices` select binding keys directly; hostname
  lookup remains an adapter for VirtualService and DestinationRule.
* Ambient projects frontends to workload-api `Service` exactly as today, but
  projects outbound-only bindings to a new internal destination view used by
  waypoints. Do **not** send opaque XBackend names to every ztunnel: ztunnel
  needs a workload-api Service only when it must capture/address a frontend.
  Waypoints need consumer-filtered outbound bindings referenced by their
  attached routes. Classic ingress gateways that route through ambient use the
  same binding endpoint resolver, including waypoint indirection where policy
  requires it.
* CDS should ultimately iterate `DestinationBinding`, not `model.Service`.
  `buildCluster`, `applyTrafficPolicy`, cluster cache keys, and
  `EndpointBuilder` receive binding identity and normalized policy. During
  migration, `model.Service` remains in `buildClusterOpts` through an adapter,
  while cache dependencies include the binding key and its source/reference/
  policy dependencies.

This avoids forcing ambient to ingest the aggregate classic registry and avoids
forcing classic xDS to understand workload-api wire objects. The shared point
is the compiled binding and endpoint collection, one layer above either output.

### Source adapters and what remains service-specific

Kubernetes Service retains ClusterIP/LoadBalancer/NodePort/headless frontend
semantics, ExternalName alias behavior, topology/local-traffic policy, service
accounts, MCS aggregation, and pod/EndpointSlice membership. Its adapter emits
frontend plus destination definitions; ExternalName emits a frontend whose
default destination has a DNS endpoint.

ServiceEntry retains multiple host/address expansion, wildcard and
`resolution: NONE`, auto-allocation, WorkloadEntry/selector membership,
mesh-internal/external location, status addresses, and ServiceEntry visibility.
Its adapter may emit several frontend/definition pairs from one config.

Workload and WorkloadEntry remain endpoint sources, not destination bindings.
DestinationRule remains a policy source and subset definition; it must not
become the identity of a destination. Subsets select endpoint metadata from an
already resolved binding.

InferencePool keeps endpoint-picker/extension configuration and inference
routing semantics, but emits `ExtensionResolved` plus connection/filter
metadata. Once CDS and route compilation consume bindings, remove the shadow
Service, inference labels on `model.Service`, and VirtualService `Extra` map in
separate compatibility steps.

### Compatibility and staged migration

1. **Extract the contract.** Move/rename Gateway `BackendBinding` and
   `RuntimeBackend` into source-neutral destination types. Preserve the current
   opaque name and gateway registry behavior through an adapter. Add golden
   equality/dependency tests; no xDS behavior changes.
2. **Build DestinationIndex.** Add endpoint-source descriptors/resolvers and
   consumer visibility. Feed XBackend through it. Make the current
   `gatewaybackend.Controller` a thin classic projection. Feed XBackend
   bindings directly to waypoint outbound service/cluster selection; do not
   publish them to ztunnel frontend discovery.
3. **Migrate InferencePool.** Emit definitions/bindings from accepted route
   edges, introduce the extension endpoint resolver and typed cluster/filter
   metadata, dual-run against the shadow-Service path, compare generated xDS,
   then remove shadow reconciliation.
4. **Split existing services.** Add Kubernetes and ServiceEntry adapters that
   emit frontend plus destination definition alongside existing registries.
   Compare service selection, endpoint sets, cluster names, DestinationRule
   application, and push dependencies without switching ownership.
5. **Switch xDS consumers.** Change gateway/sidecar/waypoint outbound selection
   and CDS/EDS cache keys to bindings. Keep hostname-to-binding lookup so
   existing VirtualService, Sidecar, and DestinationRule APIs remain compatible.
   Retain `model.Service` for LDS inbound/frontends and telemetry until those
   call sites explicitly need a narrower frontend type.
6. **Collapse duplicate pipelines.** Make classic and ambient service
   projections consume the shared definitions/endpoints, then remove duplicate
   Kubernetes/ServiceEntry endpoint conversion. Delete the dedicated Gateway
   backend registry only after aggregate registry consumers no longer require
   it.

Each stage requires a feature flag and dual-computation comparison before
cutover. Migration must preserve cluster names for declared service frontends;
opaque names are allowed only for destination-only sources. Existing config
update keys should be translated to binding dependency keys so delta xDS does
not regress to full pushes.

### Test strategy

* Contract tests run every source adapter against both classic and ambient
  projections, asserting identical effective ports, endpoints, TLS, locality,
  network, health, and visibility.
* An activation matrix covers frontend-only, reference-only, both, invalid
  reference, ReferenceGrant removal, policy change, source delete/recreate,
  and last-reference removal for sidecar, router, waypoint, and ztunnel views.
* Apples-to-apples xDS tests compare pre/post CDS and EDS for Kubernetes
  ClusterIP/headless/ExternalName, ServiceEntry STATIC/DNS/DYNAMIC_DNS/NONE,
  subsets, multi-network, locality LB, and waypoint indirection.
* Negative isolation tests prove opaque destinations never appear in DNS
  capture, inbound listeners, ztunnel address responses, unrelated Sidecar
  scopes, or unrelated Gateway/waypoint clusters.
* InferencePool dual-run tests compare extension filters, endpoint-picker
  clusters, route metadata, status, and deletion behavior before removing its
  shadow Service.
* Delta tests assert dependency-specific CDS/EDS/RDS pushes for endpoint,
  policy, reference, visibility, and credential changes; cache tests include
  binding generation and consumer identity.

### Decision points

* **Decided:** generalize and rename to `DestinationBinding`; do not make
  Gateway API the owner of Istio's core destination model.
* **Decided:** frontend and destination are separate IR objects. A destination
  is never addressable merely because it exists.
* **Decided:** share compiled bindings and endpoint collections, not the
  classic `model.Service` or ambient workload-api `Service` projections.
* **Decided:** activation and visibility are consumer-scoped reference-graph
  results, not registry-global booleans.
* **Decided:** keep source-specific API semantics in adapters; converge only
  after normalization.
* **Decision required before stage 2:** whether `DestinationIndex` belongs in
  `pilot/pkg/model`, `pilot/pkg/serviceregistry`, or a new
  `pilot/pkg/destination` package. Prefer the latter if it avoids model/krt
  cycles and can be owned by bootstrap alongside ambient and aggregate indexes.
* **Decision required before stage 5:** whether cluster names remain hostname
  based plus a binding suffix, or gain a new binding-ID format. Compatibility
  argues for unchanged names for service-backed destinations and opaque names
  only for destination-only definitions.

### Open questions

* Does a binding's consumer key need proxy class, concrete proxy identity, or a
  reusable scope (Gateway, waypoint, namespace, Sidecar scope)? Per-proxy keys
  are precise but can explode cardinality; reusable scopes should be preferred.
* How are multiple route parents with different effective policies represented
  without duplicating identical endpoint resolution?
* Should ambient gain a wire-level destination-only resource, or can waypoint
  xDS consume bindings entirely inside istiod while ztunnel remains frontend
  only? The latter is sufficient for XBackend ExternalHostname but must be
  tested for future EndpointSelector and waypoint chaining.
* Where should DNS resolution occur for ambient destination-only bindings:
  waypoint Envoy, ztunnel, or istiod? The answer differs for DNS versus
  DYNAMIC_DNS and determines the endpoint-source contract.
* Can `ClientAndServer` TLS be represented directly in the cluster policy
  compiler without extending DestinationRule, and how are its two credential
  dependencies authorized and rotated?
* How should overlapping Kubernetes Service, ServiceEntry, aliases, and opaque
  runtime names resolve conflicts? Existing creation-time and registry
  precedence must be encoded explicitly rather than inherited accidentally
  from aggregate registry order.
* Which InferencePool semantics are endpoint selection versus route/filter
  behavior? The typed extension contract must be settled before deleting the
  VirtualService `Extra` escape hatch.
* Which telemetry identity should destination-only bindings report when they
  deliberately have no canonical service frontend?

### Files and functions inspected for this pass

* `pilot/pkg/model/service.go`: `Service`, `ServiceAttributes`,
  `ServiceDiscovery`, `AmbientIndexes`, `ServiceInfo`, and inference semantics.
* `pilot/pkg/model/push_context.go`: `ServiceForHostname`, `GatewayServices`,
  `ExtraWaypointServices`, service visibility, and referenced destinations.
* `pilot/pkg/serviceregistry/gatewaybackend/controller.go`: `RuntimeBackend`,
  `Controller.reconcile`, classic service/endpoint projection, and xDS updates.
* `pilot/pkg/serviceregistry/kube/conversion.go`: `ConvertService` and
  `ExternalName` handling; Kubernetes endpoint construction in
  `kube/controller/endpoint_builder.go`.
* `pilot/pkg/serviceregistry/serviceentry/conversion.go` and `controller.go`:
  service/instance conversion, DNS resolution, workload selection, and update
  publication.
* `pilot/pkg/serviceregistry/ambient/services.go`, `ambientindex.go`, and
  `sidecar_interop.go`: Kubernetes/ServiceEntry `ServiceInfo` compilation,
  ztunnel indexes, waypoint ownership, and classic gateway EDS interop.
* `pilot/pkg/networking/core/cluster.go`, `cluster_cache.go`,
  `cluster_traffic_policy.go`, `waypoint.go`, and `listener_waypoint.go`:
  outbound service selection, DestinationRule lookup, CDS/cache construction,
  waypoint inbound/outbound selection, and service lookup.
* `pilot/pkg/xds/endpoints/endpoint_builder.go`: CDS/EDS endpoint projection and
  dependency tracking.
* `pilot/pkg/config/kube/gateway/conversion.go`, `route_collections.go`,
  `controller.go`, and `inferencepool_collection.go`: BackendRef translation,
  active XBackend bindings, InferencePool shadow Service lifecycle, status, and
  route metadata hacks.
* `pilot/pkg/serviceregistry/aggregate/controller.go` and
  `pilot/pkg/bootstrap/server.go`: registry aggregation and lifecycle wiring.

## Migration implementation log: ambient, waypoint, and InferencePool

### Stage A: InferencePool dual representation

InferencePool compilation now produces a source-neutral
`DestinationDefinition` and one `DestinationBinding` per accepted Gateway and
target port. The endpoint source is `ExtensionResolved`; it retains the
InferencePool source key, endpoint-picker service and port, failure mode,
selector-resolved source identity, UID boundary, and existing shadow hostname.
Standalone KRT collection helpers expose definitions and bindings for joining
into the shared `DestinationIndex`.

**Decision:** preserve the shadow hostname as `RuntimeName` during dual-run so
cluster and route names can be compared apples-to-apples. This does not make it
a frontend; addressability remains a property only of the legacy shadow
Service projection.

**Decision:** the binding consumer is the accepted Gateway object, including
when that Gateway is deployed as a waypoint. `Waypoint` describes the dataplane
projection, not a second authorization/activation identity.

**Still active compatibility path:** the controller continues to reconcile the
headless Kubernetes shadow Service, and inference cluster/filter handling still
reads its labels and VirtualService `Extra`. Removing either before the shared
index owns CDS, EDS, and typed extension metadata would change behavior.

### Stage B: waypoint outbound projection boundary

Waypoint core now has a focused adapter from consumer-filtered
`ResolvedDestination` values to the legacy `model.Service` plus endpoint view.
The adapter is deliberately local to outbound CDS/EDS inputs and does not add
the opaque runtime name to `AmbientIndexes` or workload-api `Service` output.
As a result, the migration has a concrete path for waypoint outbound clusters
without teaching ztunnel that destination-only bindings are frontends.

**Open integration question:** global ownership must supply the waypoint's
accepted Gateway `ConsumerID`, query `DestinationIndex.ForConsumer`, and make
the projected endpoints available to the existing endpoint builder for the
same push snapshot. This remains unwired until `DestinationIndex` ownership is
installed alongside aggregate and ambient indexes.

### Stage B integration update

The Gateway controller now owns and exposes `DestinationIndex`, so outbound
CDS performs the consumer query directly through the existing
`PushContext.GatewayAPIController` reference. A gateway or waypoint proxy's
`gateway.networking.k8s.io/gateway-name` workload label plus namespace maps to
the accepted Gateway `ConsumerID`. Only that consumer's bindings are projected
to legacy outbound `model.Service` inputs; duplicate registry projections are
suppressed by namespaced hostname.

For waypoints, the projection is appended after normal outbound-service
filtering because the accepted reference edge is itself the activation proof.
For routers and gateway-labeled sidecars, it is added to ordinary outbound CDS
input. Proxies without a Gateway identity, including ztunnel, do not query or
receive the binding. No projection is inserted into `AmbientIndexes`, address
information, workload-api `Service`, inbound listener discovery, or DNS
capture.

**Resolved:** no package refactor was required. Networking core can type-assert
the existing model-level Gateway controller to a narrow destination-index
provider. This avoids importing the destination subpackage into `model`, which
would create a package cycle, while keeping the optional capability compatible
with fake and alternate Gateway controllers.

**Remaining endpoint bridge:** DNS and Dynamic DNS destination-only bindings
can use the projected service directly. `ExtensionResolved`, static, and
service-membership bindings still need their resolved endpoint snapshot wired
into `EndpointIndex` (or CDS/EDS changed to consume `ResolvedDestination`
directly) before their compatibility registry/shadow endpoint path can be
removed.

### Stage C: InferencePool endpoint resolution and cutover gate

The destination contract now carries typed `Semantics` and
`ExtensionFailureMode` fields. InferencePool definitions use
`InferencePoolSemantics`, and the classic projection translates that capability
to the existing `UseInferenceSemantics` compatibility label. This avoids
inferring behavior from `Source.Kind` while preserving the current single
cluster, override-host, and all-target-port behavior during xDS migration.

`ExtensionResolved` is now registered in the global index. Its InferencePool
resolver watches Pods, applies the pool's selector and readiness gate, and emits
resolved endpoints with all Pod IPs, target port, labels, SPIFFE service
account, TLS mode, locality, network, workload, node, namespace, cluster, and
health metadata. Definitions remain inert without an accepted Gateway binding,
and the resolver output is consumer filtered by the index.

**Decision:** retain shadow Service reconciliation at this stage. Although
endpoint resolution is now independent of the Service, classic EDS still reads
`EndpointIndex`; it does not consume the endpoint snapshot held by
`ResolvedDestination`. The temporary Gateway backend registry also only
projects DNS destinations. Deleting the shadow Service before either bridging
resolved endpoints into `EndpointIndex` or changing EDS to consume bindings
would produce an InferencePool cluster with no endpoints.

**Parity gate before removal:** compare the new Pod resolver against
EndpointSlice conversion for terminating/serving readiness, dual-stack address
selection, network lookup beyond explicit Pod labels, custom locality overrides,
discoverability policy, workload-name metadata, and endpoint update push/cache
dependencies. The shadow path may be removed only after these are equal or the
binding endpoint path becomes authoritative for EDS.

**Still active compatibility path:** VirtualService `Extra` continues to carry
per-route endpoint-picker configuration and the inference ext-proc filter still
uses that typed Go payload. Moving this data into destination metadata requires
route compilation to select metadata by binding key; it is separate from
endpoint discovery and remains an explicit follow-up.

### Endpoint fidelity and inherited protocol follow-up

InferencePool locality now comes from the selected Pod's Kubernetes Node,
using the standard region/zone labels plus Istio subzone, rather than reading
topology labels from the Pod. Node reads are KRT dependencies of the resolved
destination, so relabeling or moving a workload recomputes endpoint locality.
Tests deliberately put an incorrect region label on the Pod and verify that the
Node-derived locality wins.

**Remaining endpoint parity gap:** the direct resolver preserves explicit Pod
network labels, but it does not yet invoke the Kubernetes registry's mesh
network lookup or platform-specific locality override provider because those
are controller-owned dependencies not exposed as reusable conversion inputs.
The shadow Service remains the authoritative EDS path until the shared resolver
receives those dependencies or the mature Kubernetes endpoint converter is
extracted behind a source-neutral helper. Terminating/serving EndpointSlice
conditions and discoverability policy remain part of the same cutover gate.

An XBackend whose `spec.protocol` is omitted now inherits protocol from each
accepted route edge: HTTPRoute becomes HTTP, GRPCRoute becomes gRPC, and TCPRoute
or TLSRoute becomes TCP. Explicit XBackend protocol continues to override this
inference. The inherited protocol is written to the binding port, connection
policy, and policy portion of `BindingKey`, allowing one Gateway to hold
distinct bindings when routes with different effective protocols reference the
same XBackend.

**Decision:** inheritance is a consumer-binding concern, not a destination
definition default. The source definition therefore retains `Unsupported` for
an omitted protocol; it cannot know the effective route/listener context.

**Open question:** if multiple HTTP-family listeners require finer upstream
distinctions than route kind provides (for example HTTP/1.1 versus HTTP/2), the
activation edge must carry listener section/protocol information. Route kind is
the most precise context in the current ancestor graph.

**Open InferencePool question:** `EndpointSource` identifies the InferencePool
and endpoint picker, allowing a resolver to read the selector from the source
object. Before shadow removal, the extension resolver must reproduce
EndpointSlice readiness, locality, network, and target-port behavior currently
inherited from Kubernetes Service discovery.

### Validation

Focused tests cover deterministic per-Gateway InferencePool bindings,
extension-resolver metadata preservation, target ports, compatibility runtime
names, and waypoint classic projection. Full package validation is required
after the shared destination contract/index files finish converging; during
this stage the shared working tree temporarily contained duplicate in-progress
contract definitions and could not compile independently.

### Stage 1 implementation record: source-neutral contract extraction

The source-neutral contract now lives in `pilot/pkg/model/destination`. Package
cycle analysis confirmed that the child package can depend on `pilot/pkg/model`
for `ConfigKey` and endpoint compatibility types; `pilot/pkg/model` does not
import the child package. This keeps the IR out of Gateway translation without
forcing it into the already broad root model package.

Concrete decisions:

* `DefinitionID` uses `model.ConfigKey`, UID, and source port identity. Binding
  identity combines that definition with a reusable `ConsumerID` and optional
  effective-policy identity.
* `EndpointSource` is a closed, comparable tagged value. It identifies DNS,
  dynamic DNS, static, service-membership, and extension resolvers without
  embedding informer objects or mutable endpoint lists.
* `ReferenceEdge` records referencer, consumer, destination, port, and object
  reference authorization. It deliberately does not imply credential or
  traffic authorization.
* `ConnectionPolicy` begins with normalized protocol and TLS intent. The
  Gateway adapter temporarily retains the API object needed by the existing
  DestinationRule compiler, while emitting normalized settings on the core
  binding.
* Dependencies are stable, sorted `model.ConfigKey` values. This is an explicit
  identity contract for later delta-xDS integration rather than an unordered
  set embedded in krt objects.
* The Gateway `BackendBinding` remains as a compatibility adapter for status
  and API-specific policy compilation, but embeds the canonical
  `DestinationBinding`. The Gateway backend registry's `RuntimeBackend` is now
  an alias of the core compatibility projection and is constructed from the
  canonical binding.

Open questions carried into stage 2:

* The exact normalized representation for multiple CA references, independent
  client credentials, load balancing, retry, and session policy must be fixed
  before those policies stop using source-specific compatibility fields.
* `ConsumerID.Cluster` is available for multi-cluster scoping, but the first
  XBackend compiler intentionally keys activation at Gateway scope. Stage 2
  must decide when cluster identity becomes semantically required.
* Static endpoint values stay resolver-owned; stage 2 must define the stable
  resolver key and endpoint collection rather than adding slices to
  `EndpointSource`.

Validation for this stage covers endpoint descriptor comparability, stable
definition/binding naming, binding equality, normalized dependency ordering,
the classic runtime adapter, Gateway binding activation, connection policy,
and Gateway backend registry lifecycle.

### Stage 4 implementation record: Kubernetes Service dual-run adapter

`pilot/pkg/serviceregistry/kube.ConvertServiceToDestinationIR` now provides a
side-effect-free dual-run adapter beside the existing `ConvertService`. It
returns one `FrontendDefinition` and one `DestinationDefinition` per service
port; no controller, aggregate registry, or bootstrap ownership changes in this
stage.

Concrete decisions:

* `FrontendDefinition` is the shared source-neutral addressability contract. It
  preserves hostname, cluster VIPs, default/explicit addresses, frontend ports,
  exact classic resolution, export visibility, selectors, service accounts,
  external addresses and ports, node-local/readiness behavior, source identity,
  and lifecycle metadata. Its explicit optional key supports sources such as a
  ServiceEntry that expand one config into multiple frontends.
* A Kubernetes Service emits a destination per source port. Port name is the
  stable identity when present; unnamed ports use protocol and number. The
  frontend links those definitions explicitly through `DefaultDestinations`.
* ClusterIP and headless Services use a `ServiceMembership` endpoint-source
  descriptor keyed by the Service and cluster. Their distinct VIP/passthrough
  behavior remains frontend resolution and does not change endpoint ownership.
* ExternalName keeps the Kubernetes Service FQDN as its frontend and emits a
  DNS destination whose target is `spec.externalName`. The external target is
  not promoted to a frontend.
* The adapter intentionally derives from the existing `ConvertService` during
  dual-run. This makes existing Kubernetes annotation, traffic-distribution,
  address, and protocol conversion authoritative while apples-to-apples tests
  establish the split contract.

Open questions carried into the switch stage:

* EndpointSlice membership still needs a resolver keyed by Service, cluster,
  and port identity before these definitions can replace the Kubernetes
  controller's endpoint path.
* MCS aggregation and multi-cluster VIP merging happen after local Service
  conversion today. The shared index must define whether merged frontend state
  is an adapter input or a projection-time operation.
* Traffic distribution and topology policy are preserved by the classic
  frontend conversion but are not yet normalized into destination connection
  policy; their endpoint-selection boundary remains to be designed.

Validation compares the IR directly with `ConvertService` for ClusterIP,
headless, and ExternalName Services, including frontend identity, VIPs,
resolution, external targets, ports, selectors, external addresses,
node-local behavior, and publish-not-ready semantics.

### Global index cutover record

Destination source compilation and index ownership are now separate lifecycle
steps. Gateway translation exports `DestinationSources` (frontends,
definitions, bindings, and resolvers); service-controller bootstrap explicitly
initializes the one global index after both Gateway and ServiceEntry controllers
exist. The controller retains an index pointer only as the compatibility
provider used by waypoint generation. This removes the previous hidden,
Gateway-constructor-owned index lifecycle without changing the
`PushContext.GatewayAPIController` interface yet.

Kubernetes Service source wiring now participates in that source graph:

* Each Service is converted through `ConvertServiceToDestinationIR` and emits
  its frontend and per-port definitions as krt collections.
* Each declared frontend activates its default destinations with a reusable
  mesh consumer scope. This is distinct from XBackend and InferencePool route
  activation: declared Kubernetes frontends are intentionally eager.
* Kubernetes bindings retain the Service hostname as their runtime identity;
  they do not enter the Gateway backend compatibility registry. That registry
  filters explicitly to XBackend definitions, preventing duplicate Kubernetes
  Services in the aggregate registry.
* Bootstrap initialization retains resolver plugins supplied by source
  adapters. Unsupported endpoint descriptors fail closed in the shared index.
* ServiceEntry frontends, definitions, bindings, and resolver plugins are joined
  into this same index at service-controller assembly. Its local dual-run index
  remains a comparison surface, not the only ServiceEntry integration path.

Decisions:

* Service-controller bootstrap, rather than Gateway translation, owns the index
  initialization boundary. Initialization occurs after ServiceEntry controller
  construction because its resolver closures depend on the controller's KRT
  instance collections. The current Gateway controller stores the resulting
  pointer only to avoid a broad environment/PushContext interface change during
  cutover.
* `ConsumerID{Kind: "Mesh"}` is the initial activation scope for declared
  Kubernetes Service frontends. Sidecar visibility filtering remains with the
  classic registry until service selection switches to the destination index.
* Classic projection remains source-filtered during migration. Adding a
  definition to the global index must not implicitly add a second registry
  frontend.

Open questions:

* Move the index pointer from `GatewayAPIController` to a dedicated Environment
  destination provider once sidecar and waypoint consumers switch together.
* Kubernetes `ServiceMembership` needs the shared EndpointSlice resolver before
  global resolved destinations can replace the existing EDS path. Until then,
  its source wiring is dual-run and fails closed at resolution.
* Namespace annotations are not yet available to the global source adapter's
  pure conversion callback. Export/traffic-distribution parity requiring
  namespace defaults must be wired before switching ownership.
* Mesh-scoped bindings must eventually be filtered through export visibility,
  Sidecar egress hosts, cluster tenancy, and waypoint ownership rather than
  being interpreted as globally visible by new consumers.

### Destination-native EDS generation and lifecycle

Resolved destinations now carry enough endpoint and runtime metadata to
generate EDS without finding a registry or shadow `model.Service`. The shared
index publishes destination-owned endpoint shards, while the EDS generator
uses the proxy's consumer identity plus runtime hostname and port to retrieve
the corresponding `ResolvedDestination`. It creates a request-local classic
projection and transient endpoint index, then runs the standard
`EndpointBuilder`; neither object is registered or globally discoverable.

Decisions:

* Runtime lookup is consumer-scoped. A hostname/port-only global lookup was
  rejected because two Gateway or waypoint consumers may compile different
  endpoint or policy views for the same runtime name.
* Destination-owned endpoint slices run through the standard EndpointBuilder,
  preserving dual-stack additional addresses, endpoint locality, health,
  load-balancing weight, network/discoverability filtering, mTLS and telemetry
  metadata, locality failover, and DestinationRule subset behavior. Resolver
  output is already consumer-filtered; global service-registry membership is
  not consulted again.
* Endpoint publication collapses duplicate consumer bindings into one
  destination shard. On last-binding removal it first publishes an empty EDS
  shard and then sends the service-delete lifecycle notification. This ordering
  prevents stale endpoints and lets the delete remove the now-empty shard.
* Sources still owned by legacy registries remain filtered out of the
  destination EDS publisher during dual-run. InferencePool is the first native
  EDS owner; expanding the filter is an explicit source cutover.

Open questions:

* Subset selection and DestinationRule-derived endpoint filtering still use the
  legacy EndpointBuilder. Destination-native subset metadata and normalized
  traffic policy must land before native EDS handles non-empty subset names.
* Multi-network gateway rewriting, waypoint tunneling, failover priority, and
  panic-threshold policy require proxy-aware normalized metadata. The direct
  generator currently preserves resolved endpoint metadata but does not
  independently repeat those legacy service-policy transforms.
* Mesh consumers currently use one reusable consumer scope. Sidecar-scope
  identity must be introduced before sidecars can have divergent resolved views
  for an otherwise identical runtime hostname.

Regression coverage proves consumer-isolated runtime lookup, locality/health/
weight/TLS metadata generation without a Service, populated destination shard
publication, duplicate binding collapse, and empty-shard removal after the last
binding disappears.

### Stage 2 implementation record: destination index and endpoint resolution

`pilot/pkg/model/destination.Index` now joins active `DestinationBinding`
objects to their `DestinationDefinition`, dispatches a closed endpoint-source
descriptor to a resolver, and publishes `ResolvedDestination` objects. A
definition without a binding remains inert. Missing or ambiguous definitions
and endpoint kinds without a registered resolver fail closed.

Concrete decisions:

* `NewIndex` accepts joined definition and binding collections rather than
  owning source registration. Source adapters remain ordinary KRT collections
  and global ownership can combine them with `krt.JoinCollection`.
* DNS and dynamic-DNS are built-in resolvers. Service membership, static, and
  extension resolution require explicit plugins that own their source watches.
* Resolver-returned source keys are merged with the definition source and
  binding dependencies into a stable delta-xDS dependency list. KRT reads made
  through the resolver context retain normal collection-level invalidation.
* `ForConsumer` performs exact consumer lookup plus an optional `ConsumerSet`
  match. It never treats an active binding as mesh-visible. Gateway-backed
  waypoints use the accepted Gateway object as their consumer identity.
* `ProjectClassic` is the compatibility boundary for `model.Service` and
  `model.IstioEndpoint`. Producing this value does not publish a frontend; the
  caller decides whether it enters an aggregate registry or only a local
  sidecar/gateway/waypoint cluster view.
* Effective endpoint and port data come from the binding. The definition is
  still joined to enforce lifecycle and supplies reusable source identity and
  resolver context.

Open questions carried into integration:

* Global index ownership and collection joining must be placed where classic
  aggregate and ambient indexes can share it without creating a package cycle.
* Service-membership and extension resolvers must define whether an empty
  endpoint result is a valid empty cluster or a compilation error; the index
  currently publishes a resolved destination with zero endpoints.
* `ConsumerID.Cluster` must be populated before multi-cluster definitions can
  safely share runtime names.
* Classic projection currently marks destination-only services mesh-external.
  Kubernetes Service and mesh-internal ServiceEntry adapters need an explicit
  source-neutral location field before they adopt this projection.

Validation covers inactive definitions, accepted-edge activation and
last-reference removal, consumer isolation, deterministic dependencies,
resolver plugin dependencies, unsupported-source fail-closed behavior, DNS
endpoint resolution, and the classic compatibility projection.

### Stage 4A implementation record: ServiceEntry dual-run adapter

`pilot/pkg/serviceregistry/serviceentry.ConvertToDestinationInputs` now emits
shared `FrontendDefinition` and `DestinationDefinition` values without
registering them or changing ownership of the existing ServiceEntry controller.
This gives stage 4 an apples-to-apples dual-run seam before any cutover.

Concrete decisions:

* ServiceEntry's host/address Cartesian product remains frontend-only. Each
  host/port pair emits one reusable destination, so adding another VIP does not
  duplicate endpoint or connection intent.
* Destination identity includes the ServiceEntry source, UID, hostname, and
  port name. This preserves delete/recreate boundaries and prevents multiple
  hosts in one ServiceEntry from collapsing into one cluster identity.
* `STATIC` uses `StaticEndpoints`, `DNS` and `DNS_ROUND_ROBIN` use `DNS`, and
  `DYNAMIC_DNS` uses `DynamicDNS`. Exact round-robin behavior remains on the
  frontend's `model.Resolution` and as endpoint-source extension metadata.
* `NONE` is represented as `ExtensionResolved` with the explicit
  `serviceentry/passthrough` discriminator. Treating it as static or DNS would
  incorrectly imply an endpoint load-balancing mode.
* Explicit IPs and CIDRs follow the current conversion's validation and /32 or
  /128 normalization. Addressless frontends retain `UnspecifiedIP`; automatic
  VIP allocation remains owned by the existing registry during dual-run.
* Export visibility, workload selector, mesh location, subject alternative
  names/service accounts, target ports, creation time, resource version, and
  source dependencies are retained in the shared projections.

Open questions before ServiceEntry cutover:

* Static and DNS endpoint resolvers must reproduce WorkloadEntry, inline
  endpoint, selector, locality, network, health, and target-port behavior from
  `convertServiceEntryToInstances`; this adapter intentionally emits only the
  resolver descriptor.
* Auto-allocated IPv4/IPv6 addresses depend on the allocation controller and
  namespace/config context. Global ownership must enrich frontend projections
  from that collection rather than duplicating allocation in this pure adapter.
* `DNS_ROUND_ROBIN` needs an explicit normalized connection/resolution field
  before CDS stops consulting the frontend compatibility projection.
* Labels, traffic distribution, DNS connect strategy, passthrough target-port
  maps, and canonical-service label synthesis need shared frontend metadata
  before the legacy `model.Service` projection can be byte-for-byte complete.

Comparison tests cover representative STATIC, DNS, DNS_ROUND_ROBIN,
DYNAMIC_DNS, and NONE entries against `convertServices`, including host/address
expansion, invalid-address filtering, port/protocol/target-port retention,
visibility, selectors, SANs, mesh location, and frontend resolution.

### Stage 4B implementation record: ServiceEntry resolver wiring

The ServiceEntry controller now builds source-neutral frontend, definition,
and mesh binding collections beside its existing `ServiceWithInstances`
collection. A controller-owned `DestinationIndex` resolves those bindings from
the same converted instances that currently feed EDS, making the shared view
authoritative for ServiceEntry destination data while legacy registry
publication remains enabled for compatibility.

Concrete decisions:

* Declared ServiceEntry frontends activate one mesh binding per host/port
  definition. Multiple addresses do not duplicate bindings or endpoints.
* Static, DNS, dynamic-DNS, and passthrough resolver plugins all read the
  existing `ServiceWithInstances` KRT collection. This deliberately reuses the
  mature inline endpoint, WorkloadEntry selector, locality, network, health,
  and target-port conversion during cutover instead of reimplementing it.
* Resolver lookup keys include namespace and hostname, then filter by the
  originating ServiceEntry object name and effective port. Two ServiceEntries
  sharing a hostname therefore retain independent destination identities and
  endpoint sets.
* DNS resolver registration preserves non-ServiceEntry behavior with a direct
  hostname fallback, allowing these plugins to be joined into an index that
  also contains XBackend DNS definitions.
* The ServiceEntry source `ConfigKey` is an explicit resolved dependency.
  WorkloadEntry and selected-workload changes are additionally tracked by KRT
  fetch dependencies on `ServiceWithInstances`.
* `NONE` produces a valid resolved binding with the converted endpoint set,
  which may be empty. Its `Passthrough` cluster semantics remain encoded by the
  frontend resolution and endpoint-source discriminator rather than invented
  EDS behavior.

Open questions before removing the legacy registry:

* Global ownership must join the controller's destination collections and
  resolver plugins with Gateway, Kubernetes, and InferencePool sources. The
  controller-local index proves authority and lifecycle but is not yet the one
  CDS/EDS entrypoint.
* KRT invalidation is precise at the converted ServiceEntry collection level,
  but resolved dependency metadata cannot identify individual inline endpoint
  entries. WorkloadEntry keys should be propagated explicitly if delta-xDS
  later requires object-level attribution beyond collection fetches.
* ExportTo visibility is retained on frontends but mesh bindings currently use
  a broad `ConsumerSet{Kind: Mesh}` compatibility scope. Global consumer
  selection must intersect frontend visibility and SidecarScope before cutover.
* DNS round-robin deduplication across multiple ServiceEntries with the same
  host and port still lives in `mergeServicesInstancesByNamespaceHost`; the
  per-source destination view intentionally does not collapse those identities.

Tests cover static, DNS, and NONE endpoint parity, port and target-port
selection, source dependency propagation, same-host source isolation, final
source deletion, and destination collection lifecycle.

### Resolver ownership correction

Global resolver registration is keyed by `ResolverKey`, not only by
`EndpointSourceKind`. The key contains the endpoint kind, source API kind, and
an optional extension discriminator. Resolution tries the most specific key
first, then source-scoped, extension-scoped, and finally kind-only fallback.

This fixes a confirmed collision in which ServiceEntry passthrough and
InferencePool both registered `ExtensionResolved`; bootstrap's map merge made
the last source silently own both APIs. ServiceEntry now owns only
`ExtensionResolved/ServiceEntry/serviceentry/passthrough`, while InferencePool
owns `ExtensionResolved/InferencePool`. Built-in DNS remains a kind-only
fallback, and ServiceEntry DNS overrides it only for ServiceEntry definitions.

**Decision:** resolver identity follows the definition source, not the binding
or registration order. Bindings may carry effective endpoint data, but they
cannot change which source adapter is authorized to resolve a definition.

**Open question:** extension discriminators are exact strings today. If a
source introduces versioned or parameterized extension names, it should either
register source-kind ownership or add an explicit stable resolver class rather
than rely on prefix matching.

Focused tests join ServiceEntry and InferencePool definitions using the same
endpoint kind and prove each receives endpoints from its intended resolver.

### Integration checkpoint: shared consumption and cutover boundary

The Gateway API controller now owns an interim `destination.Index` assembled
from XBackend and InferencePool definitions and bindings. XBackend's dedicated
classic registry is projected from resolved index entries instead of compiling
the Gateway binding directly. Waypoint CDS also asks this index for the
accepted Gateway consumer and appends only that consumer's destination-only
classic projections. This is the first real consumer of the common model, not
only a dual-write comparison path.

Concrete decisions:

* Route activation remains represented by `DestinationBinding`; neither a
  definition nor its compatibility projection creates a mesh-wide frontend.
* Gateway and waypoint use the accepted Gateway object as the consumer key.
  This keeps a referenced backend private to the data plane that owns the
  referencing routes.
* The existing `gatewaybackend` registry remains a temporary endpoint/EDS
  bridge for XBackend. Its runtime input is now a compatibility projection of
  the common resolved destination rather than a second backend model.
* InferencePool emits definitions and bindings into the same index, but its
  `ExtensionResolved` source intentionally fails closed until a resolver owns
  the pool endpoint data. Its synthetic Service remains authoritative during
  this dual-run period.
* Kubernetes Service and ServiceEntry adapters are deliberately not joined to
  the Gateway-owned index. Doing so before global ownership and consumer rules
  exist would create a second partially authoritative mesh registry.
* Sidecars without a matching consumer binding see no destination-only
  projection. Normal Kubernetes Service and ServiceEntry behavior continues
  through the existing aggregate registry, so this checkpoint does not change
  their data-plane semantics.

Open questions for the next cutover:

* Move index ownership out of the Gateway controller so Kubernetes Service,
  ServiceEntry, Gateway API, ambient, and sidecar sources share one lifecycle
  and dependency graph.
* Implement ServiceMembership, StaticEndpoints, and ExtensionResolved plugins
  that publish endpoint data directly to the endpoint index/EDS path. This is
  the prerequisite for deleting both the XBackend registry bridge and the
  InferencePool shadow Service.
* Define consumer identities for ordinary sidecars and ambient workloads,
  including namespace/export visibility, ReferenceGrant authorization, and
  multi-cluster identity, before broadening `ForConsumer` beyond gateways.
* Decide whether normal Services use explicit bindings, an indexed default
  visibility binding, or a separate frontend reachability relation. Encoding
  mesh visibility as one binding per workload would be prohibitively large.
* Normalize topology, traffic distribution, DNS round-robin, passthrough,
  target-port, and mesh location behavior before replacing classic CDS and EDS
  projections for Kubernetes Service and ServiceEntry.

Validation at this checkpoint includes alpha-enabled Gateway, networking-core,
and bootstrap tests plus the complete `pilot/pkg/...` suite. The implementation
is therefore an end-to-end XBackend/common-index slice and a dual-run migration
for the other sources, not yet the final global registry cutover.

### Stage D: destination-owned EDS and InferencePool shadow removal

The global destination index now publishes endpoint membership into Pilot's
existing `EndpointIndex` through a dedicated `Destination` shard. Publication
is selected by typed destination semantics; sources that still publish through
legacy Kubernetes or ServiceEntry registries are not duplicated. InferencePool
is the first source to make this path authoritative.

Concrete decisions:

* EDS publication groups by runtime hostname and namespace, then collapses
  duplicate consumer bindings and combines endpoints from every target port.
  This preserves InferencePool's one-cluster, all-target-port behavior.
* The publisher sends the complete endpoint set through `XDSUpdater.EDSUpdate`.
  When the final binding disappears it sends an empty update and a service
  delete for its own shard, preventing stale EndpointIndex keys.
* The destination shard uses the control-plane cluster ID. Endpoint locality,
  network, workload identity, readiness, dual-stack addresses, and target port
  are supplied by the InferencePool resolver before publication.
* Route translation reads endpoint-picker service, port, failure mode, runtime
  hostname, and first target port directly from the InferencePool API. It no
  longer discovers those values through labels or dummy ports on a synthetic
  Kubernetes Service.
* Classic projection remains consumer-local: CDS gets the destination-only
  service for an accepted Gateway, while EDS reads the same runtime hostname
  from the destination-owned shard. No frontend is added to ambient address
  discovery or the aggregate service registry.
* The shadow-Service reconciler, server-side apply path, ownership labels, and
  generated Service tests are removed. Existing route goldens no longer seed
  synthetic services, proving that translation does not depend on them.

Lifecycle and upgrade considerations:

* A shadow Service created by an older Istiod remains owned by its
  InferencePool and is garbage-collected when that pool is deleted. This
  implementation does not proactively delete it during a mixed-revision
  upgrade, because an older revision would recreate it and the controllers
  would fight. New and updated pools do not create shadow Services.
* Kubernetes Service and ServiceEntry remain on their existing EDS publishers
  until their destination projections are promoted independently. The EDS
  filter is explicit to prevent double-sharding during that migration.

Validation covers multi-consumer deduplication, multi-port aggregation, final
binding deletion, direct route translation without a shadow Service, typed
inference semantics, endpoint metadata, and the existing Gateway conversion
goldens.

### Stage E: native EDS lookup and compatibility-registry removal

The destination index is now sufficient to generate both CDS and EDS without a
globally registered `model.Service`. EDS resolves an already-issued cluster by
consumer, runtime hostname, and port, builds a request-local classic projection,
and runs destination-owned endpoints through the standard `EndpointBuilder`.
The projection is adapter metadata for one xDS request; it never enters
`PushContext.Services`, aggregate service discovery, ambient address discovery,
or a persistent shadow registry.

Decisions:

* Runtime lookup is consumer-aware. A Gateway can resolve only its own binding;
  an unrelated sidecar cannot use a runtime hostname to discover another
  consumer's destination.
* Destination endpoints use the standard endpoint-generation path rather than
  a reduced protobuf converter. This preserves dual-stack addresses,
  discoverability and network filtering, mTLS metadata, locality weighting and
  failover, and applicable DestinationRule behavior.
* CDS carries consumer-scoped destination endpoints alongside its request-local
  service projections. DNS clusters therefore resolve an XBackend's external
  hostname rather than its opaque runtime hostname; the endpoint never has to
  enter `PushContext` service discovery.
* Resolver ownership is keyed by endpoint kind plus source kind and optional
  extension discriminator. Registration order cannot change which adapter owns
  an InferencePool or ServiceEntry definition.
* The `gatewaybackend` service registry and `RuntimeBackend` projection are
  deleted. XBackend activation can no longer add a service to aggregate
  discovery, so route-local activation remains private to its Gateway consumer.
* An omitted XBackend protocol is inherited from each accepted route edge.
  HTTPRoute, GRPCRoute, and TCPRoute/TLSRoute variants receive distinct runtime
  hostnames and therefore distinct Envoy cluster identities; an explicit
  XBackend protocol remains authoritative.
* InferencePool locality is derived from Node topology. Pod topology labels do
  not override the Node, while explicit Istio network metadata on the workload
  remains available to endpoint generation.

Open questions:

* InferencePool selection still originates from the local Pod/Node collections.
  A shared multicluster workload index should become a resolver input before
  claiming remote-cluster InferencePool endpoint parity.
* Kubernetes EndpointSlice serving/terminating semantics and platform-specific
  locality overrides are not yet represented by the generic selector resolver.
  These belong in the shared workload adapter rather than source-specific
  copies.
* `DestinationIndex` is still exposed through the Gateway API controller while
  bootstrap assembles global sources. Moving ownership to an environment-level
  component would make the source-neutral lifecycle explicit.

Validation for this stage covers resolver collision, route protocol inheritance
and identity, Node-derived locality, consumer-aware runtime lookup, normal
EndpointBuilder output without a registered Service, route activation/removal,
and absence of the GatewayBackend aggregate registry.
