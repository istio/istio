# Istio Config

This component handles the configuration, exposing an MCP server.

The default implementation is Galley, using the K8S apiserver for storage - other MCP providers may be configured.

It is recommended to run only one production config server - it registers a validation webhook which will apply
to all Istio configs. It is possible to run a second staging/canary config server in a different namespace.

## Installation

Galley relies on DNS certificates. Before installing it in a custom namespace you should update Citadel or
create a custom certificate.

## Validation

A cluster should have a single galley with validation enabled - usually the prod environment.
It is possible to enable validation on other environments as well - but each Galley will do its own
validation, and a staging version may impact production validation.

```yamml
security:
    ...
    dnsCerts:
        ...
        istio-galley-service-account.MY_NAMESPACE: istio-galley.MY_NAMESPACE.svc
```
