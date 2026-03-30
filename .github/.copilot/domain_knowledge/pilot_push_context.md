# Pilot PushContext

This document explains the purpose, construction, and usage of the `PushContext` in Istio's Pilot component, focusing on how it is built and transformed into xDS messages for Envoy proxies.

## Introduction

`PushContext` is a core data structure in Istio Pilot that encapsulates the full state required to generate configuration for Envoy proxies. It is central to the process of translating Istio's high-level configuration into concrete xDS resources delivered to Envoy.

## Conceptual Overview

- `PushContext` represents a snapshot of the mesh configuration at a point in time.
- It is constructed during a push event (e.g., config change, endpoint update).
- The context is used to generate xDS resources (Clusters, Listeners, Routes, Endpoints) for all connected Envoy proxies.
- It enables efficient, consistent, and parallelized config generation.

## Implementation Architecture

1. **Trigger**: A config/event change triggers a push in Pilot.
2. **Construction**: Pilot builds a new `PushContext` by aggregating mesh config, service registry, endpoints, and policies.
3. **xDS Generation**: The `PushContext` is passed to xDS generators, which produce Envoy resources.
4. **Delivery**: xDS resources are sent to Envoy via the discovery service.

### Key Relationships
- `PushContext` is owned by the `Environment` (see `pilot/pkg/model/environment.go`).
- xDS generators (e.g., `CdsGenerator`, `LdsGenerator`) consume `PushContext`.
- Each push event creates a new `PushContext` instance.

## Code Implementation

### Construction

```go
// From pilot/pkg/model/push_context.go
func (ps *PushContext) Init(
    env *Environment,
    mesh *meshconfig.MeshConfig,
    ... // other params
) error {
    // Loads services, endpoints, virtual services, destination rules, etc.
    ps.initServiceRegistry(env)
    ps.initVirtualServices(env)
    ps.initDestinationRules(env)
    // ... more initialization
    return nil
}
```

### Usage in xDS Generation

```go
// From pilot/pkg/xds/eds.go
func (s *DiscoveryServer) generateEndpoints(
    proxy *model.Proxy,
    push *model.PushContext,
    ...
) []*endpoint.ClusterLoadAssignment {
    // Uses push.Services, push.ServiceInstances, etc.
    // to build ClusterLoadAssignment for Envoy
}
```

## Key Interfaces/Models

- `PushContext` struct: `pilot/pkg/model/push_context.go`
- `Environment`: Holds the current mesh state, owns `PushContext`
- xDS Generators: `pilot/pkg/xds/`

## Example Use Cases

1. **Config Change**: User updates a VirtualService. Pilot triggers a push, builds a new `PushContext`, and generates new xDS resources for affected Envoys.
2. **Endpoint Update**: Kubernetes endpoint changes. Pilot updates the service registry, builds a new `PushContext`, and pushes updated endpoints to Envoy.

## Best Practices

- Always use the current `PushContext` for xDS generation to ensure consistency.
- Avoid mutating `PushContext` after construction; treat it as immutable.
- Use `PushContext` methods to access mesh state (services, virtual services, etc.) instead of accessing raw data.
- Profile and optimize `PushContext` construction for large meshes.

## Related Components
