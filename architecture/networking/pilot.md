# Architecture of Istiod

This document describes the high level architecture of the Istio control plane, Istiod.
Istiod is structured as a modular monolith, housing a wide range of functionality from certificate signing, proxy configuration (XDS), traditional Kubernetes controllers, and more.

## Proxy Configuration

Istiod's primary role - and most code - is to dynamically configure proxies (Envoy sidecars and ingress, gRPC, ztunnel, and more). This roughly consists of 3 parts:
1. Config ingestion (inputs to the system)
1. Config translation
1. Config serving (XDS)

### Config Ingestion

Istio reads from over 20 different resources types, and aggregates them together to build the proxy configuration. These resources can be sourced from Kubernetes (via watches), files, or over xDS; Kubernetes is by far the most common usage, though.

Primarily for historical reasons, ingestion is split into a few components.

#### ConfigStore

The `ConfigStore` reads a variety of resources and exposes them over a standard interface (Get, List, etc). These types are wrapped in a common `config.Config` struct, contrasting with typical Kubernetes clients which use per-resource types. The most common is reading from Kubernetes via the `crdclient` package.

```mermaid
graph TD
    subgraph ConfigStore
        xcs(XDS Client)
        ccs(CRD Client)
        fcs(Filesystem Client)
        acs(Aggregate)
        xcs-->acs
        ccs-->acs
        fcs-->acs
    end
```

#### ServiceDiscovery

The other primary interface is the ServiceDiscovery. Similar to ConfigStore, this aggregates over a variety of resources. However, it does not provide generic resource access, and instead precomputes a variety of service-oriented internal resources, such as `model.Service` and `model.ServiceInstance`.

This is composed of two controllers - one driven from core Kubernetes types ("Kube Controller") and one by Istio types ("ServiceEntry controller").

```mermaid
graph TD
    subgraph Kube Controller
        s(Services)
        e(Endpoints)
        p(Pods)
        ksi(ServiceInstances)
        kwi(WorkloadInstances)
        s-->ksi
        e-->ksi
        p-->kwi
    end
    subgraph ServiceEntry Controller
        se(ServiceEntry)
        we(WorkloadEntry)
        ssi(ServiceInstances)
        swi(WorkloadInstances)
        se-->ssi
        swi-->ssi
        we-->swi
    end
    kwi-->ssi
    swi-->ksi
```

For the most part this is fairly straight forward. However, we support `ServiceEntry` selecting `Pod`, and `Service` selecting `WorkloadEntry`, which leads to cross-controller communication.

Note: the asymmetry with `Pods` not contributing to Kube controller's `ServiceInstances` is due to the use of `Endpoints` which itself is derived from `Pod` from Kubernetes core.

#### PushContext

`PushContext` is an immutable snapshot of the current state of the world. It is regenerated (usually partially) on each configuration push (more on this below). Due to being a snapshot, most lookups are lock-free.

`PushContext` is built up by querying the above layers. For some simple use cases, this is as simple as storing something like `configstore.List(SomeType)`; in this case, the only difference from directly exposing the configstore is to snapshot the current state. In other cases, some pre-computations and indexes are computed to make later accesses efficient.

#### Endpoints

Endpoints have an optimized code path, as they are by far the most frequently updated resource - in a steady cluster, this will often be the *only* change, caused by scale up/down.

As a result, they do not go through `PushContext`, and changes do not trigger a `PushContext` recomputation. Instead, the current state is incrementally computed based on events from `ServiceDiscovery`.

#### Conclusion

Overall, the high level config ingestion flow:

```mermaid
graph TD
    sd(Service Discovery)
    cs(ConfigStore)
    ep(Endpoints)
    pc(PushContext)
    sd-->pc
    cs-->pc
    sd-->ep
```

### Config Translation

Config Translation turns the above inputs into the actual types consumed by the connected XDS clients (typically Envoy). This is done by `Generators`, which register a function to build a given type. For example, there is a `RouteGenerator` responsible for building `Routes`. Along with the core Envoy XDS types, there are a few custom Istio types, such as our `NameTable` type used for DNS, as well as debug interfaces.

`Generators` get as input the `Proxy` (a representation of the current client), the current `PushContext` snapshot, and a list of config updates that caused the change.

The `Proxy` as an input parameter is important, and a major distinction from some other XDS implementations. We are not able to statically translate inputs to XDS without per-client information. For example, we rely on the client's labels to determine the set of policies applied. While this is necessary to implement Istio's APIs, it does limit performance substantially.

#### Caching

Config translation typically takes the overwhelming majority of Istiod's resource usage. In particular, protobuf encoding. As a result, caching has been introduced, storing the already encoded `protobuf.Any` for a given resource.

This caching depends on declaring all inputs to the given generator as part of the cache key. This is extremely error-prone, as there is nothing preventing generators from consuming inputs that are *not* part of the key. When this happens, different clients will non-deterministically get incorrect configuration. This type of bug has historically resulted in CVEs.

There are a few ways to prevent these:
* Only pass in to the generation logic the cache key itself, so no other unaccounted inputs can be used. Unfortunately, this has not been done for any generators today.
* Be very, very careful.
* The cache has a builtin test, enabled with `UNSAFE_PILOT_ENABLE_RUNTIME_ASSERTIONS=true`, that runs in CI. This will panic if any key is written to with a different value.

#### Partial Computations

Along with caching, partial computations are a critical performance optimization to ensure that we do not need to build (or send) every resource to every proxy on every change. This is discussed more in the Config Serving section.

### Config Serving

Config serving is the layer that actually accepts proxy clients, connected over bidirectional gRPC streams, and serve them the required configuration.

We will have two triggers for sending config - requests and pushes.

#### Requests

Requests come from the client specifically asking for a set of resources. This could be requesting the initial set of resources on a new connection, or from a new dependency. For example, a push of `Cluster X` referencing `Endpoint Y` may lead to a request for `Endpoint Y` if it is not already known to the client.

Note that clients can actually send three types of messages - requests, ACKs of previous pushes, and NACKs of previous pushes. Unfortunately, these are not clearly distinguished in the API, so there is some logic to split these out (`shouldRespond`).

#### Pushes

A push occurs when Istiod detects an update of some set of configuration is needed. This results in roughly the same result as a Request (new configuration is pushed to the client), and is just triggered by a different source.

Various components described in Config Ingestion can trigger a Config Update. These are batched up ("debounced"), to avoid excessive activity when many changes happen in succession, and eventually enqueued in the Push Queue.

The Push Queue is mostly a normal queue, but it has some special logic to merge push requests for each given proxy. This results in each proxy having 0 or 1 outstanding push requests; if additional updates come in the existing push request is just expanded.

Another job polls this queue and triggers each client to start a push.

```mermaid
graph TD
    subgraph Config Flow
        cu(Config Update)
        db(Debounce)
        pc(Recompute Push Context)
        pq(Push Queue)
        cu-->db
        db--Trigger Once Steady-->pc
        pc--Enqueue All Clients-->pq
    end
    subgraph Proxy
        c(Client)
    end
    subgraph Pusher
        pj(Push Job)
        pj--read-->pq
        pj--trigger-->c
    end
```

At a high level, each client job will find the correct generator for the request, generate the required configuration, and send it.

#### Optimizations

A naive implementation would simply regenerate all resources, of all subscribed types, for each client, on any configuration change. However, this scales poorly. As a result, we have many levels of optimizations to avoid doing this work.

First, we have a concept of a `Full` push. Only `Full` pushes will recompute `PushContext` on change; otherwise this is skipped and the last `PushContext` is reused. Note: even when `Full`, we try to copy as much from the previous `PushContext` as possible. For example, if only a `WasmPlugin` changed, we would not recompute services indexes.
Note: `Full` only refers to whether a `PushContext` recomputation is needed. Even within a `Full` push, we keep track of which configuration updates triggered this, so we could have "Full update of Config X" or "Full update of all configs".

Next, for an individual proxy we will check if it could possibly be impacted by the change. For example, we know a sidecar never is impacted by a `Gateway` update, and we can also look at scoping (from `Sidecar.egress.hosts`) to further restrict update scopes.

Once we determine the proxy may be impacted, we determine which *types* may be impacted. For example, we know a `WasmPlugin` does not impact the `Cluster` type, so we can skip generating `Cluster` in this case. Warning: Envoy currently has a bug that *requires* `Endpoints` to be pushed any time the corresponding `Cluster` is pushed, so this optimization is intentionally turned off in this specific case.

Finally, we determine which subset of the type we need to generate. XDS has two modes - "State of the World (SotW)" and "Delta". In SotW, we generally need to generate all resources of the type, even if only one changed. Note that we actually need to *generate* all of them, typically, as we do not store previously generated resources (mostly because they are generated per-client). This also means that whenever we are determining if a change is required, we are doing this based on careful code analysis, not at runtime.
Despite this expectation in SotW, due to a quirk in the protocol we can actually enable one of our most important optimizations. XDS types form a tree, with CDS and LDS the root of the tree for Envoy. For root types, we *must* always generate the full set of resources - missing resources are treated as deletions.
However, all other types *cannot* be deleted explicitly, and instead are cleaned up when all references are removed. This means we can send partial updates for non-root types, without deleting unsent resources. This effectively allows doing delta updates over SotW. This optimization is critical for our endpoints generator, ensuring that when a pod scales we only need to update the endpoints within that pod.

Istio currently supports both SotW and Delta protocol. However, the delta implementation is not yet optimized well, so it performs mostly the same as SotW.

## Controllers

Istiod consists of a collection of controllers. Per Kubernetes, "controllers are control loops that watch the state of your cluster, then make or request changes where needed."

In Istio, we use the term a bit more liberally. Istio controllers watch more than just the state of *a* cluster -- many are reading from multiple clusters, or even external sources (files and XDS). Generally, Kubernetes controllers are then writing state back to the cluster; Istio does have a few of these controllers, but most of them are centered around driving the [Proxy Configuration](#proxy-configuration).

### Writing controllers

Istio provides a few helper libraries to get started writing a controller. While these libraries help, there are still a lot of subtleties in correctly writing (and testing!) a controller properly.

To get started writing a controller, review the [Example Controller](../../pkg/kube/controllers/example_test.go).

### Controllers overview

Below provides a high level overview of controllers in Istiod. For more information about each controller, consulting the controllers Go docs is recommended.

```mermaid
graph BT
    crd("CRD Watcher")
    subgraph Service Discovery
        ksd("Kubernetes Controller")
        sesd("Service Entry Controller")
        msd("Memory Controller")
        asd("Aggregate")
        ksd--Join-->asd
        sesd--Join-->asd
        msd--Join-->asd
        ksd<--"Data Sharing"-->sesd
    end
    subgraph ConfigStore
        ccs("CRD Client")
        xcs("XDS Store")
        fcs("File Store")
        mcs("Memory Store")
        acs("Aggregate")
        ccs--Join-->acs
        xcs--Join-->acs
        fcs--Join-->acs
        mcs--Join-->acs
    end
    subgraph VMs
        vmhc("Health Check")
        vmar("Auto Registration")
    end
    subgraph Gateway
        twc("Tag Watcher")
        gdc("Gateway Deployment")
        gcc("Gateway Class")
        twc--Depends-->gdc
        gdc-.-gcc
    end
    subgraph Ingress
        ic("Ingress Controller")
        isc("Ingress Status Controller")
        ic-.-isc
    end
    mcsc("Multicluster Secret")
    scr("Credentials Controller")
    mcsc--"1 per cluster"-->scr
    mcsc--"1 per cluster"-->ksd
    crd--Depends-->ccs

    iwhc("Injection Webhook")
    vwhc("Validation Webhook")
    nsc("Namespace Controller")
    ksd--"External Istiod"-->nsc
    ksd--"External Istiod"-->iwhc

    df("Discovery Filter")

    axc("Auto Export Controller")

    mcfg("Mesh Config")
    dfc("Default Revision Controller")
```

As you can see, the landscape of controllers is pretty extensive at this point.

[Service Discovery](#ServiceDiscovery) and [Config Store](#ConfigStore) were already discussed above, so do not need more explanation here.

#### Mesh Config

Mesh Config controller is a pretty simple controller, reading from `ConfigMap`(s) (multiple if `SHARED_MESH_CONFIG` is used), processing and merging these into a the typed `MeshConfig`. It then exposes this over a simple `mesh.Watcher`, which just exposes a way to access the current `MeshConfig` and get notified when it changes.

#### Ingress

In addition to `VirtualService` and `Gateway`, Istio supports the `Ingress` core resource type. Like CRDs, the `Ingress` controller implements `ConfigStore`, but a bit differently. `Ingress` resources are converted on the fly to `VirtualService` and `Gateway`, so while the controller reads `Ingress` resources (and a few related types like `IngressClass`), it emits other types. This allows the rest of the code to be unaware of Ingress and just focus on the core types

In addition to this conversion, `Ingress` requires writing the address it can be reached at in status. This is done by the Ingress Status controller.

#### Gateway

Gateway (referring to the [Kubernetes API](http://gateway-api.org/), not the same-named Istio type) works very similarly to [Ingress](#ingress). The Gateway controller also converts Gateway API types into `VirtualService` and `Gateway`, implementing the `ConfigStore` interface.

However, there is also a bit of additional logic. Gateway types have extensive status reporting. Unlike Ingress, this is status reporting is done inline in the main controller, allowing status generation to be done directly in the logic processing the resources.

Additionally, Gateway involves two components writing to the cluster:
* The Gateway Class controller is a simple controller that just writes a default `GatewayClass` object describing our implementation.
* The Gateway Deployment controller enables users to create a Gateway which actually provisions the underlying resources for the implementation (Deployment and Service). This is more like a traditional "operator". Part of this logic is determining which Istiod revision should handle the resource based on `istio.io/rev` labeling (mirroring sidecar injection); as a result, this takes a dependency on the "Tag Watcher" controller.

#### CRD Watcher

For watches against custom types (CRDs), we want to gracefully handle missing CRDs. Naively starting informers against the missing types would result in errors and blocking startup. Instead, we introduce a "CRD Watcher" component that watches the CRDs in the cluster to determine if they are available or not.

This is consumed in two ways:
* Some components just block on `watcher.WaitForCRD(...)` before doing the work they need.
* `kclient.NewDelayedInformer` can also fully abstract this away, by providing a client that handles this behind the scenes.

#### Credentials Controller

The Credentials controller exposes access to TLS certificate information, stored in cluster as `Secrets`. Aside from simply accessing certificates, it also has an authorization component that can verify whether a requester has access to read `Secret`s in its namespace.

#### Discovery Filter

The Discovery Filter controller is used to implement the `discoverySelectors` field of `MeshConfig`. This controller reads `Namespace`s in the cluster to determine if they should be "selected". Many controllers consumer this filter to only process a subset of configurations.

#### Multicluster

Various controllers read from multiple clusters.

This is rooted in the Multicluster Secret controller, which reads `kubeconfig` files (stored as `Secrets`), and creating Kubernetes clients for each. The controller allows registering handlers which can process Add/Update/Delete of clusters.

This has two implementations:
* The Credentials controller is responsible for reading TLS certificates, stored as Secrets.
* The Kubernetes Service Discovery controller is a bit of a monolith, and spins off a bunch of other sub-controllers in addition to the core service discovery controller.

Because of the monolithic complexity it helps to see this magnified a bit:

```mermaid
graph BT
    mcsc("Multicluster Secret")
    scr("Credentials Controller")
    ksd("Kubernetes Service Controller")
    nsc("Namespace Controller")
    wes("Workload Entry Store")
    iwh("Injection Patcher")
    aex("Auto Service Export")
    scr-->mcsc
    ksd-->mcsc
    nsc-->ksd
    wes-->ksd
    iwh-->ksd
    aex-->ksd
```

#### VMs

Virtual Machine support consists of two controllers.

The Auto Registration controller is pretty unique as a controller - the inputs to the controller are XDS connections. In response to each XDS connection, a `WorkloadEntry` is created to register the XDS client (which is generally `istio-proxy` running on a VM) to the mesh. This `WorkloadEntry` is tied to the lifecycle of the connection, with some logic to ensure that temporary downtime (reconnecting, etc) does not remove the `WorkloadEntry`.

The Health Check controller additionally controls the health status of the `WorkloadEntry`. The health is reported over the XDS client and synced with the `WorkloadEntry`.

#### Webhooks

Istio contains both Validation and Mutating webhook configurations. These need a `caBundle` specified in order to provision the TLS trust. Because Istiod's CA certificate is somewhat dynamic, this is patched at runtime (rather than part of the install). The webhook controllers handle this patching.

These controllers are very similar but are distinct components for a variety of reasons.
