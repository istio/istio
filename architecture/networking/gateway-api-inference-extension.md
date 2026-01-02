# Gateway API Inference Extension (GIE)

This document describes Istio's support for the [Gateway API Inference Extension](https://gateway-api-inference-extension.sigs.k8s.io/).

## Overview

The Gateway API Inference Extension enables intelligent request routing to machine learning inference workloads. It allows dynamic endpoint selection using an Endpoint Picker Protocol (EPP) service, which can make intelligent decisions about which backend pod should handle each request based on factors like model availability, load, or request characteristics.

## Key Components

### InferencePool

The `InferencePool` is a Kubernetes CRD from the `inference.networking.k8s.io/v1` API group that represents a pool of inference model server endpoints. Key fields include:

- `selector`: Label selector to identify model server pods in the pool
- `targetPorts`: List of ports exposed by the model servers (supports multiple ports as of GIE v1.1.0)
- `endpointPickerRef`: Reference to an external service that provides endpoint selection logic

Example:

```yaml
apiVersion: inference.networking.k8s.io/v1
kind: InferencePool
metadata:
  name: my-inference-pool
spec:
  targetPorts:
  - number: 8000
  - number: 8001
  selector:
    matchLabels:
      app: inference-workload
  endpointPickerRef:
    name: endpoint-picker-service
    port:
      number: 9002
```

### Endpoint Picker Protocol (EPP)

The EPP is an external gRPC service that selects specific endpoints for requests. It uses Envoy's ext_proc filter for request interception and dynamic endpoint selection. The EPP service receives request headers and returns the selected endpoint via the `x-endpoint` header in the format `<pod-ip>:<port>`.

### Shadow Service

For each InferencePool, Istio automatically creates an internal "shadow" Service:

- Named pattern: `<pool-name>-ip-<hash>` (e.g., `test-pool-ip-a1b2c3d4`)
- Headless service (ClusterIP: None)
- Labels for EPP configuration:
    - `istio.io/inferencepool-name`: Pool name
    - `istio.io/inferencepool-extension-service`: EPP service name
    - `istio.io/inferencepool-extension-port`: EPP service port
    - `istio.io/inferencepool-extension-failure-mode`: Failure mode (FailOpen/FailClose)

## How It Works

1. An HTTPRoute references an InferencePool as a backend:

   ```yaml
   backendRefs:
     - group: inference.networking.k8s.io
       kind: InferencePool
       name: my-inference-pool
       port: 80
   ```

1. The Gateway controller detects this and creates a shadow Service

1. During route conversion, the ext_proc filter is attached with EPP service details

1. At runtime, Envoy uses ext_proc to query the EPP service for endpoint selection

1. Requests are routed to the selected pod:port combination

## Enabling the Feature

The Gateway API Inference Extension is disabled by default. To enable it:

1. Ensure `PILOT_ENABLE_GATEWAY_API=true` (required)
1. Set `ENABLE_GATEWAY_API_INFERENCE_EXTENSION=true` on istiod

Example:

```yaml
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  values:
    pilot:
      env:
        PILOT_ENABLE_GATEWAY_API: "true"
        ENABLE_GATEWAY_API_INFERENCE_EXTENSION: "true"
```

## Running Tests

The integration tests live in `tests/integration/pilot/gie`.

To run the GIE integration tests:

```bash
go test -tags=integ ./tests/integration/pilot/gie/... -v
```

The tests require:

- A Kubernetes cluster with Istio installed
- Gateway API CRDs installed
- Gateway API Inference Extension CRDs installed

## Test Scenarios

### TestInferencePoolMultipleTargetPorts

This test verifies:

- InferencePools with multiple targetPorts create a single shadow service
- The shadow service has correct labels and is headless
- EPP endpoint selection works correctly for all configured ports
- Traffic is routed to the correct pod:port based on EPP response

## Related Resources

- [Gateway API Inference Extension Specification](https://gateway-api-inference-extension.sigs.k8s.io/)
- [Issue #55768](https://github.com/istio/istio/issues/55768) - Initial GIE support
- [Issue #57638](https://github.com/istio/istio/issues/57638) - Multiple targetPorts support
