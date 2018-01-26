# Istio API Style Guide

This page defines the design guidelines for Istio APIs. They apply to
all proto files in the Istio project. Developers who create their APIs
using Istio may find these guidelines useful as well.

Since Istio APIs are based on _proto3_ and _gRPC_, we will use
Google's [API Design Guide](https://cloud.google.com/apis/design) as
the baseline. Because Envoy APIs also uses the same baseline, the
commonality across Envoy, Istio, proto3 and gRPC will greatly help
developer experience in the long term.

In addition to Google's guide, the following conventions should be
followed for Istio APIs.

## Versioning

When defining Kubernetes Custom Resource Definition (CRD) using
proto3, the proto versioning should match the Kubernetes versioning,
see the following example.

The Kubernetes CRD:

```yaml
apiVersion: config.istio.io/v1alpha1
kind: Authorization
```

The proto message definition:
```proto
package istio.config.v1alpha1;
message Authorization {...}
```
