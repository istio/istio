
This Proxy will use Envoy and talk to Mixer server. 

## Build Mixer server

* Follow https://github.com/istio/mixer/blob/master/doc/devel/development.md to set up environment, and build via:

```
  cd $(ISTIO)/mixer
  bazel build ...:all
```
  
## Build Envoy proxy

* Build target envoy:

```
  bazel build //src/envoy/mixer:envoy
```

## How to run it

* Start mixer server. In mixer folder run:

```
  bazel-bin/cmd/server/mixs server
    --globalConfigFile testdata/globalconfig.yml
    --serviceConfigFile testdata/serviceconfig.yml  --logtostderr
```
  
  The server will run at port 9091

* Start backend Echo server.

```
  cd test/backend/echo
  go run echo.go
```

* Start Envoy proxy, run

```
  src/envoy/mixer/start_envoy
```
  
* Then issue HTTP request to proxy.

```
  curl http://localhost:9090/echo -d "hello world"
```

## How to configurate HTTP filters

This module has two HTTP filters:
1. mixer filter: intercept all HTTP requests, call the mixer.
2. forward_attribute filter: Forward attributes to the upstream istio/proxy.

### *mixer* filter:

This filter will intercept all HTTP requests and call Mixer. Here is its config:

```
   "filters": [
      "type": "both",
      "name": "mixer",
      "config": {
         "mixer_server": "${MIXER_SERVER}",
         "attributes" : {
            "attribute_name1": "attribute_value1",
            "attribute_name2": "attribute_value2"
         }
    }
```

Notes:
* mixer_server is required
* attributes: these attributes will be send to the mixer

### *forward_attribute* HTTP filter:

This filer will forward attributes to the upstream istio/proxy.

```
   "filters": [
      "type": "decoder",
      "name": "forward_attribute",
      "config": {
         "attributes": {
            "attribute_name1": "attribute_value1",
            "attribute_name2": "attribute_value2"
 	    }
    }
```

Notes:
* attributes: these attributes will be forwarded to the upstream istio/proxy.



