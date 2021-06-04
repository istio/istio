# echogen

`echogen` is a util for generating Kubernetes manifests from echo configurations.

## Overview

### Installation

```bash
go install istio.io/pkg/test/framework/components/echo/echogen
```

### Usage

```text
echogen [opts] config.yaml
```

The config file is YAML containing a list of
[echo.Config](https://github.com/istio/istio/blob/master/pkg/test/framework/components/echo/config.go#L52) objects:

```yaml
- Service: a
  Namespace: echo
- Service: headless
  Namespace: echo
  Headless: true
```

### Options

`echogen` supports all options from the test framework that would affect Echo deployments
such as: `istio.test`, `.imagePullSecret`, `istio.test.hub` and several others.

In addition to the framework level options:

```text
-out <file>: Write output to the specified file
-dir: If specified, each deployment will be written to a separate file, in a directory named by -out.
```

### Full Example with gRPC UI

1. Make sure to install [gRPC UI](https://github.com/fullstorydev/grpcui) if you haven't already

1. Create an `echogen` config:

```bash
echo '
- Service: a
  Namespace: echo
- Service: b
  Namespace: echo
' > config.yaml
```

1. Run `echogen`:

```bash
echogen -out echo.yaml config.yaml
```

1. Apply the manifest to the cluster

```bash
kubectl apply -f echo.yaml
```

1. Port-forward the gRPC port (default container port is 17070)

```bash
kubectl -n echo port-forward a-v1-fc649d9fc-59rkj 17070
```

1. Start gRPC UI

```bash
grpcui -plaintext localhost:17070
```

1. Because our echo gRPC service enables reflection, you should be able to open your browser
   and get a user interface that shows all of the possible methods and request options.

   Change the "Method name" to `ForwardEcho`, then in "Request Data" set `url` to `grpc://b:7070` then click `Invoke`

1. (Bonus) If you open the "Raw Request (JSON)" tab, you can re-use that for requests via
   [grpcurl](https://github.com/fullstorydev/grpcurl) without constructing JSON by hand:

```bash
grpcurl -plaintext localhost:17070 EchoTestService/ForwardEcho -d '{"url": "grpc://b:7070"}'
```
