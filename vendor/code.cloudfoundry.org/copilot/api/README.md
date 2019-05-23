# copilot api

This package holds the golang gRPC API definitions for copilot.  There's one API used by Cloud Controller, another used by Istio Pilot.

The actual gRPC `.proto` files are in the `protos` subdirectory.  To regenerate the golang code, run `go generate ./...`.

We moved the `.proto` files in there to work around problems with Bazel in Istio.
