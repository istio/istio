# Kubernetes Declarative Controller Runtime (`krt`)

The `krt` package provides a high-level, declarative framework for building controllers in Istio. It abstracts away much of the boilerplate and complexity of traditional Kubernetes controllers, enabling developers to focus on transformation logic and relationships between resources.

## Overview

`krt` enables the construction of controllers that operate on arbitrary object types from any source, not just Kubernetes resources. It provides a set of primitives for building and composing collections of objects, supporting efficient, reactive, and dependency-aware computation.

## Architecture

- **Collections**: The core abstraction, similar to an informer, but not tied to Kubernetes. Collections can be built from informers, static data, or derived from other collections.
- **Transformations**: Functions that map input objects to output objects, supporting one-to-one, one-to-many, and singleton patterns.
- **Dependency Tracking**: Automatically tracks dependencies between collections and recomputes outputs only when necessary.
- **Filtering**: Provides efficient, composable filters for querying collections by key, label, namespace, etc.
- **Debugging**: Integrated debug handler for tracing and introspection.

## Implementation Details

### Key Patterns

#### Creating a Collection from an Informer

```go
// From pilot/pkg/controllers/untaint/nodeuntainter.go
nodes := krt.WrapClient[*v1.Node](n.nodesClient, opts.WithName("nodes")...)
pods := krt.WrapClient[*v1.Pod](n.podsClient, opts.WithName("pods")...)
```

#### One-to-One Transformation

```go
// From pilot/pkg/controllers/untaint/nodeuntainter.go
readyCniPods := krt.NewCollection(pods, func(ctx krt.HandlerContext, p *v1.Pod) **v1.Pod {
    if p.Namespace != n.ourNs { return nil }
    if !n.cnilabels.SubsetOf(p.ObjectMeta.Labels) { return nil }
    if !IsPodReadyConditionTrue(p.Status) { return nil }
    return &p
}, opts.WithName("cni-pods")...)
```

#### One-to-Many Transformation

```go
// From pkg/kube/krt/README.md
Endpoints := krt.NewManyCollection[Endpoint](func(ctx krt.HandlerContext, svc *v1.Service) (res []Endpoint) {
    for _, pod := range krt.Fetch(ctx, Pods, krt.FilterLabel(svc.Spec.Selector)) {
        res = append(res, Endpoint{Service: svc.Name, Pod: pod.Name, IP: pod.Status.PodIP})
    }
    return res
})
```

#### Singleton Collection

```go
// From pkg/kube/krt/README.md
ConfigMapCount := krt.NewSingleton[int](func(ctx krt.HandlerContext) *int {
    cms := krt.Fetch(ctx, ConfigMaps)
    return ptr.Of(len(cms))
})
```

#### Status Collection

```go
// From pilot/pkg/config/kube/gateway/gatewayclass_collection.go
return krt.NewStatusCollection(gatewayClasses, func(ctx krt.HandlerContext, obj *gateway.GatewayClass) (*gateway.GatewayClassStatus, *GatewayClass) {
    // ...status computation...
}, opts.WithName("GatewayClasses")...)
```

### Component Flow

1. **Source Data**: Collections are created from informers, static data, or other collections.
2. **Transformation**: Functions transform input objects to outputs, possibly querying other collections.
3. **Dependency Tracking**: The framework tracks dependencies and only recomputes outputs when relevant inputs change.
4. **Event Handling**: Consumers can register handlers to react to changes in collections.

## Key Files

- `pkg/kube/krt/collection.go`: Core collection logic and dependency management.
- `pkg/kube/krt/filter.go`: Filtering primitives for querying collections.
- `pkg/kube/krt/informer.go`: Integration with Kubernetes informers.
- `pkg/kube/krt/static.go`: Static collections.
- `pkg/kube/krt/status.go`: Status collection support.
- `pkg/kube/krt/util.go`, `helpers.go`: Utility functions and helpers.
- `pkg/kube/krt/README.md`: In-depth documentation and usage patterns.

## Examples

### Example: Node Untainter Controller

```go
// From pilot/pkg/controllers/untaint/nodeuntainter.go
opts := krt.NewOptionsBuilder(stop, "node-untaint", debugger)
nodes := krt.WrapClient[*v1.Node](n.nodesClient, opts.WithName("nodes")...)
pods := krt.WrapClient[*v1.Pod](n.podsClient, opts.WithName("pods")...)

readyCniPods := krt.NewCollection(pods, func(ctx krt.HandlerContext, p *v1.Pod) **v1.Pod {
    // ...filter logic...
    return &p
}, opts.WithName("cni-pods")...)

readyCniNodes := krt.NewCollection(readyCniPods, func(ctx krt.HandlerContext, p *v1.Pod) **v1.Node {
    pnode := krt.FetchOne(ctx, nodes, krt.FilterKey(p.Spec.NodeName))
    // ...filter logic...
    return &node
}, opts.WithName("ready-cni-nodes")...)

readyCniNodes.Register(func(o krt.Event[*v1.Node]) {
    if o.New != nil {
        n.queue.AddObject(*o.New)
    }
})
```

### Example: GatewayClass Status Collection

```go
// From pilot/pkg/config/kube/gateway/gatewayclass_collection.go
return krt.NewStatusCollection(gatewayClasses, func(ctx krt.HandlerContext, obj *gateway.GatewayClass) (*gateway.GatewayClassStatus, *GatewayClass) {
    // ...status computation...
}, opts.WithName("GatewayClasses")...)
```

## Best Practices

- Use the most specific collection type (`NewCollection`, `NewManyCollection`, `NewSingleton`) for your use case.
- Always use `krt.Fetch` to access other collections within transformation functions.
- Ensure transformation functions are stateless and idempotent.
- Use provided filters for efficient querying.
- Avoid querying external state or making side effects in transformation functions.
- Register event handlers to react to changes in collections.

## Related Components

- [PushContext](pilot_push_context.md): Traditional configuration snapshotting in Istio.
- [istioctl_commands.md](istioctl_commands.md): How Istio's CLI interacts with controllers.
- [istio_analysis_messages.md](istio_analysis_messages.md): Diagnostics and analysis patterns.

---

This file should be updated as `krt` evolves and as new usage patterns emerge in the Istio codebase.
