
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
  bazel build //src/envoy/prototype:envoy_esp
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
  npm install
  node echo.js
```

* Start Envoy proxy, run

```
  bazel-bin/src/envoy/prototype/envoy_esp -c src/envoy/prototype/envoy-esp.conf
```
  
* Then issue HTTP request to proxy.

```
  curl http://localhost:9090/echo?key=API-KEY -d "hello world"
```

## How to add attributes or facts

Now only some of attributes are passed to mixer.  If you want to add more attributes, you can
modify this [file](https://gcp-apis.git.corp.google.com/esp/+/test/envoy-mixer/src/api_manager/mixer/mixer.cc).
