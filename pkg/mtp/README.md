# Mesh Transport Protocol

Mesh Transport Protocol (MTP) is the protocol used by Istio.
At a high level, the protocol consists of tunneling TCP connections over HTTP/2 CONNECT, over mTLS.

## Specification

TODO

## Implementations

### Clients

#### CLI

A binary client is available using the `client` binary.

Usage examples:

```shell
go install ./pkg/test/echo/cmd/client
# Send request to 127.0.0.1:8080 (Note only IPs are supported) via an MTP proxy on port 15008
client --mtp-client-cert tests/testdata/certs/cert.crt --mtp-client-key tests/testdata/certs/cert.key \
  http://127.0.0.1:8080 \
  --mtp 127.0.0.1:15008
```

#### Golang

An (unstable) library to make MTP connections is available at `pkg/mtp`.

Usage example:

```go
d := NewDialer(Config{
    ProxyAddress: "1.2.3.4:15008",
    Headers: map[string][]string{
        "some-addition-metadata": {"test-value"},
    },
    TLS:          nil, // No TLS for simplification
})
client, _ := d.Dial("tcp", testAddr)
client.Write([]byte("hello world"))
```

### Server

TODO