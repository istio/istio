# HTTP Based Overlay Network (HBONE)

HTTP Based Overlay Network (HBONE) is the protocol used by Istio for communication between workloads in the mesh.
At a high level, the protocol consists of tunneling TCP connections over HTTP/2 CONNECT, over mTLS.

## Specification

TODO

## Implementations

### Clients

#### CLI

A CLI client is available using the `client` binary.

Usage examples:

```shell
go install ./pkg/test/echo/cmd/client
# Send request to 127.0.0.1:8080 (Note only IPs are supported) via an HBONE proxy on port 15008
client --hbone-client-cert tests/testdata/certs/cert.crt --hbone-client-key tests/testdata/certs/cert.key \
  http://127.0.0.1:8080 \
  --hbone 127.0.0.1:15008
```

#### Golang

An (unstable) library to make HBONE connections is available at `pkg/hbone`.

Usage example:

```go
d := hbone.NewDialer(hbone.Config{
    ProxyAddress: "1.2.3.4:15008",
    Headers: map[string][]string{
        "some-addition-metadata": {"test-value"},
    },
    TLS:          nil, // TLS is strongly recommended in real world
})
client, _ := d.Dial("tcp", testAddr)
client.Write([]byte("hello world"))
```

### Server

#### Server CLI

A CLI client is available using the `server` binary.

Usage examples:

```shell
go install ./pkg/test/echo/cmd/server
# Serve on port 15008 (default) with TLS
server --tls 15008 --crt tests/testdata/certs/cert.crt --key tests/testdata/certs/cert.key
```

#### Server Golang Library

An (unstable) library to run an HBONE server is available at `pkg/hbone`.

Usage example:

```go
s := hbone.NewServer()
// TLS is strongly recommended in real world
l, _ := net.Listen("tcp", "0.0.0.0:15008")
s.Serve(l)
```
