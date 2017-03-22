
This Proxy will use Envoy and talk to Mixer server. 

## Build Mixer server

* Follow https://github.com/istio/mixer/blob/master/doc/dev/development.md to set up environment, and build via:

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
  # request to server-side proxy
  curl http://localhost:9090/echo -d "hello world"
  # request to client-side proxy that gets sent to server-side proxy
  curl http://localhost:7070/echo -d "hello world"
```

## How to configurate HTTP filters

### *mixer* filter:

This filter will intercept all HTTP requests and call Mixer. Here is its config:

```
   "filters": [
      "type": "decoder",
      "name": "mixer",
      "config": {
         "mixer_server": "${MIXER_SERVER}",
         "mixer_attributes" : {
            "attribute_name1": "attribute_value1",
            "attribute_name2": "attribute_value2",
            "quota.name": "RequestCount"
         },
         "forward_attributes" : {
            "attribute_name1": "attribute_value1",
            "attribute_name2": "attribute_value2"
         }
    }
```

Notes:
* mixer_server is required
* mixer_attributes: these attributes will be send to the mixer
* forward_attributes: these attributes will be forwarded to the upstream istio/proxy.
* "quota.name" and "quota.amount" are used for quota call. "quota.amount" is default to 1 if missing.

By default, mixer filter forwards attributes and does not invoke mixer server. You can customize this behavior per HTTP route by supplying an opaque config:

```
    "opaque_config": {
      "mixer_control": "on",
      "mixer_forward": "off"
    }
```

This config reverts the behavior by sending requests to mixer server but not forwarding any attributes.
