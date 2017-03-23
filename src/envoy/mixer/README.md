
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

## How to configurate HTTP Mixer filters

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
         },
         "quota_name": "RequestCount",
         "quota_amount": "1",
         "check_cache_keys": [
              "request.host",
              "request.path",
              "origin.user"
         ]
    }
```

Notes:
* mixer_server is required
* mixer_attributes: these attributes will be sent to the mixer in both Check and Report calls.
* forward_attributes: these attributes will be forwarded to the upstream istio/proxy. It will send them to mixer in Check and Report calls.
* quota_name, quota_amount are used for making quota call. quota_amount is default to 1 if missing.
* check_cache_keys is to cache check calls. If missing or empty, check calls are not cached.

By default, mixer filter forwards attributes and does not invoke mixer server. You can customize this behavior per HTTP route by supplying an opaque config in the route config:

```
    "opaque_config": {
      "mixer_control": "on",
      "mixer_forward": "off"
    }
```

This route opaque config reverts the behavior by sending requests to mixer server but not forwarding any attributes.


## How to enable quota (rate limiting)

Quota (rate limiting) is enforced by the mixer. Mixer needs to be configured with Quota in its global config and service config. Its quota config will have
"quota name", its limit within a window.  If "Quota" is added but param is missing, the default config is: quota name is "RequestCount", the limit is 10 with 1 second window. Essentially, it is imposing 10 qps rate limiting.

Mixer client can be configured to make Quota call for all requests.  If "quota_name" is specified in the mixer filter config, mixer client will call Quota with the specified quota name.  If "quota_amount" is specified, it will call with that amount, otherwise the used amount is 1.


## How to pass some attributes from client proxy to mixer.

Usually client proxy is not configured to call mixer (it can be enabled in the route opaque_config). Client proxy can pass some attributes to mixer by using "forward_attributes" field.  Its attributes will be sent to the upstream proxy (the server proxy). If the server proxy is calling mixer, these attributes will be sent to the mixer.
