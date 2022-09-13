# grpc-echo

This sample demonstrates Istio's Proxyless gRPC support with a special injection template `grpc-agent`.
The template injects the `istio-proxy` sidecar, but the sidecar will only run `pilot-agent` and not envoy.

See the [gRPC xDS feature status](https://github.com/grpc/grpc/blob/master/doc/grpc_xds_features.md) for more
information.
