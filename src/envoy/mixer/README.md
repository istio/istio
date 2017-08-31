
This Proxy will use Envoy and talk to Mixer server. 

## Build Mixer server

* Follow https://github.com/istio/mixer/blob/master/doc/dev/development.md to set up environment, and build via:

```
  cd $(ISTIO)/mixer
  bazel build ...:all
```
  
## Build Envoy proxy

* Follow https://github.com/lyft/envoy/blob/master/bazel/README.md to set up environment, and build target envoy:

```
  bazel build //src/envoy/mixer:envoy
```

## How to run it

* Start mixer server. In mixer folder run:

```
  bazel-bin/cmd/server/mixs server \
    --configStoreURL=fs://$(pwd)/testdata/configroot \
    --alsologtostderr
```
  
  The server will run at port 9091.
  In order to run Mixer locally, you also need to edit `testdata/configroot/scopes/global/subjects/global/rules.yml` as described in its comments.

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

## How to configurate Mixer server

In Envoy config, Mixer server has to be one of "clusters" under "cluster_manager".
For examples:
```
 "cluster_manager": {
    "clusters": [
     ...,
     {
        "name": "mixer_server",
        "connect_timeout_ms": 5000,
        "type": "strict_dns",
        "circuit_breakers": {
           "default": {
              "max_pending_requests": 10000,
              "max_requests": 10000
            }
        },
        "lb_type": "round_robin",
        "features": "http2",
        "hosts": [
          {
            "url": "tcp://${MIXER_SERVER}"
          }
        ]
     }
```
Its name has to be "mixer_server".

## How to configurate HTTP Mixer filters

This filter will intercept all HTTP requests and call Mixer. Here is its config:

```
   "filters": [
      "type": "decoder",
      "name": "mixer",
      "config": {
         "mixer_attributes" : {
            "attribute_name1": "attribute_value1",
            "attribute_name2": "attribute_value2"
         },
         "forward_attributes" : {
            "attribute_name1": "attribute_value1",
            "attribute_name2": "attribute_value2"
         },
         "quota_name": "RequestCount",
         "quota_amount": "1"
    }
```

Notes:
* mixer_attributes: these attributes will be sent to the mixer in both Check and Report calls.
* forward_attributes: these attributes will be forwarded to the upstream istio/proxy. It will send them to mixer in Check and Report calls.
* quota_name, quota_amount are used for making quota call. quota_amount defaults to 1.

## HTTP Route opaque config
By default, the mixer filter only forwards attributes and does not call mixer server. This behavior can be changed per HTTP route by supplying an opaque config:

```
 "routes": [
   {
     "timeout_ms": 0,
     "prefix": "/",
     "cluster": "service1",
     "opaque_config": {
      "mixer_control": "on",
      "mixer_forward": "off"
     }
   }
```

Above route opaque config reverts the behavior by sending requests to mixer server but not forwarding any attributes.

Mixer attributes and forward attributes can be set per-route in the route opaque config.

```
 "routes": [
   {
     "timeout_ms": 0,
     "prefix": "/",
     "cluster": "service1",
     "opaque_config": {
      "mixer_attributes.key1": "value1",
      "mixer_forward_attributes.key2": "value2"
     }
   }
```
Attribute key1 = value1 will be sent to the mixer if mixer is on. Attribute key1 = value2 will be forwarded to next proxy if mixer_forward is on.


## How to enable quota (rate limiting)

Quota (rate limiting) is enforced by the mixer. Mixer needs to be configured with Quota in its global config and service config. Its quota config will have
"quota name", its limit within a window.  If "Quota" is added but param is missing, the default config is: quota name is "RequestCount", the limit is 10 with 1 second window. Essentially, it is imposing 10 qps rate limiting.

Mixer client can be configured to make Quota call for all requests.  If "quota_name" is specified in the mixer filter config, mixer client will call Quota with the specified quota name.  If "quota_amount" is specified, it will call with that amount, otherwise the used amount is 1.

Following config will enable rate limiting with Mixer:

```
         "quota_name": "RequestCount",

```


## How to pass some attributes from client proxy to mixer.

Usually client proxy is not configured to call mixer (it can be enabled in the route opaque_config). Client proxy can pass some attributes to mixer by using "forward_attributes" field.  Its attributes will be sent to the upstream proxy (the server proxy). If the server proxy is calling mixer, these attributes will be sent to the mixer.


## How to disable cache for Check calls

Check calls can be cached. By default, it is enabled. Mixer server controls which attributes to use for cache keys. It also controls the cache expiration.

Check cache can be disabled with following config:
```
         "disable_check_cache": "true",
```

## How to disable cache for Quota calls

Quota cache is tied to Check cache. It is enabled automatically if Check cache is enabled.  It can be disabled with following config:
```
         "disable_quota_cache": "true",
```

## How to change network failure policy

When there is any network problems between the proxy and the mixer server, what should the proxy do for its Check calls?  There are two policy: fail open or fail close.  By default, it is using fail open policy.  It can be changed by adding this mixer filter config "network_fail_policy". Its value can be "open" or "close".  For example, following config will change the policy to fail close.

```
         "network_fail_policy": "close",

```
The default value is "open".

## How to configurate TCP Mixer filters

Here is its sample config:

```
   "filters": [
        {
          "type": "both",
          "name": "mixer",
          "config": {
              "mixer_attributes": {
                  "target.uid": "POD222",
                  "target.service": "foo.svc.cluster.local"
               },
               "quota_name": "RequestCount"
          }
        }
    ]
```

This filter will intercept a tcp connection:
* Call Check at connection creation and call Report at connection close.
* All mixer settings described above can be used here.


