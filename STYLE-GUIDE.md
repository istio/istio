# Istio API Style Guide

This page defines the design guidelines for Istio APIs. They apply to
all proto files in the Istio project. Developers who create their own
APIs using Istio should find these guidelines useful as well.

Since Istio APIs are based on _proto3_ and _gRPC_, we will use
Google's [API Design Guide](https://cloud.google.com/apis/design) as
the baseline. Because Envoy APIs also uses the same baseline, the
commonality across Envoy, Istio, proto3 and gRPC will greatly help
developer experience in the long term.

In addition to Google's guide, the following conventions should be
followed for Istio APIs.

## Naming

Having consistent naming improves understanding of the APIs and reduce
churns on the API design. Istio APIs should follow these naming
conventions:

* Package names must use `lowercase` without any `_`. Singular words
  are recommended to avoid mixture of plural and singular words in
  a package name. For example, `istio.network.config.v1`.

* Message/enum/method names must use `CamelCase` without any embedded
  acronyms, see [#364](https://github.com/istio/api/issues/364) for
  reasons. For example, `HttpRequest`.

* Enum values must use `UPPERCASE_WITH_UNDERSCORE`. For example,
  `INT_TYPE`.

* Field names must use `lowercase_with_underscore`. For example,
  `display_name`.

* Repeated fields must use proper plural names. For example,
  `rules`.

* Avoid using postpositive adjectives in names. For example,
  `collected_items` is recommended over `items_collected`.

## Versioning

When defining Kubernetes Custom Resource Definition (CRD) using
`proto3`, follow these guidelines:

* The proto `package` name must match the Kubernetes `apiVersion`,
  excluding the `.io` DNS suffix and reversing the DNS segment
  ordering. The Kubernetes `apiVersion` has the format of
  `group/version`.

* The proto message type must match the CRD `kind` name.

#### Example

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
