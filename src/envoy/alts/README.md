# ALTS support (experimental)

*The code in this directory is experimental. Do not use in production*

A prototype of
[ALTS](https://cloud.google.com/security/encryption-in-transit/application-layer-transport-security/)
support for Istio/Envoy. It depends on ALTS stack in gRPC library and implemented as Envoy's
[transport socket](https://www.envoyproxy.io/docs/envoy/latest/api-v2/api/v2/core/base.proto#core-transportsocket).

An example config is in `example.yaml`. Note: If you want to enable the peer validation, please
uncomment and replace the content of `peer_service_accounts` with the actual service account in your
environment. Please make sure the service account is correct otherwise the ALTS connection will be
closed due to validation failure.
