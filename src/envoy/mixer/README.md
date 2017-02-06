
This Proxy will use Envoy and talk to Mixer server. 

## Build Mixer server

* Follow https://github.com/istio/mixer/blob/master/doc/devel/development.md to set up environment, and build via:

```
  cd $(ISTIO)/mixer
  bazel build ...:all
```
  
## Build Envoy proxy

* Build target envoy_esp:

```
  bazel build //src/envoy/mixer:envoy_esp
```

## How to run it

* Start mixer server. In mixer folder run:

```
  bazel-bin/cmd/server/mixs server
```
  
  The server will run at port 9091

* Start backend Echo server.

```
  cd test/backend/echo
  go run echo.go
```

* Start Envoy proxy, run

```
  bazel-bin/src/envoy/mixer/envoy_esp -c src/envoy/prototype/envoy-mixer.conf
```
  
* Then issue HTTP request to proxy.

```
  curl http://localhost:9090/echo -d "hello world"
```

