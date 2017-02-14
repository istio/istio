# Istio Authentication Module

The idea of [service
mesh](https://docs.google.com/document/d/1RRPrDK0mEwhPb13DSyF6pODugrRTFLAXia9CZLPoQno/edit)
has been proposed that injects high-level networking functionality in
Kubernetes’ deployments by interposing Istio proxies in a transparent or
semi-transparent fashion. Istio auth leverages Istio proxies to enable strong
authentication and data security for the services’ inbound and outbound
traffic, without or with little change to the application code.

## Goals
- Secure service to service communication and end-user to service communication
  via Istio proxies.
- Provide a key management system to automate key generation, distribution, and
  rotation.
- Expose the authenticated identities for authorization, rate limiting,
  logging, monitoring, etc.

## Develop

[Bazel](https://bazel.build/) is used for build and dependency management. The
following commands build and test sources:

```bash
$ bazel build //...
$ bazel test //...
```

Bazel uses `BUILD` files to specify package dependencies and how targets are
built from the source. The
[gazelle](https://github.com/bazelbuild/rules_go/tree/master/go/tools/gazelle)
tool is used to automatically generate and update `BUILD` files:

```bash
$ gazelle -go_prefix "github.com/istio/auth" --mode fix
```
