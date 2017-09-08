# JWT Authentication Proxy

## Overview

__(TODO:figure)__


### Processing flow

Soon after the server runs:

1. This proxy run as a sidecar of the server.
2. Configure which issuers to use, via Envoy config.

Before an user sending request:

1. The user should request an issuer for an access token (JWT)
    - Note: JWT claims should contain `aud`, `sub`, `iss` and `exp`.

For every request from user client:

1. Client send an HTTP request together with JWT, which is intercepted by this proxy
2. The proxy verifies JWT:
    - The signature should be valid
    - JWT should not be expired
    - Issuer (and audience) should be valid
3. If JWT is valid, the user is authenticated and the request will be passed to the server, together with JWT payload (user identity). \
   If JWT is not valid, the request will be discarded and the proxy will send a response with an error message.


## How to build it

* Follow https://github.com/lyft/envoy/blob/master/bazel/README.md to set up environment, and build target envoy:

```
  bazel build //src/envoy/auth:envoy
```

## How to run it

* Start Envoy proxy. Run

```
bazel-bin/src/envoy/auth/envoy -c src/envoy/auth/sample/envoy.conf
```

* Start backend Echo server.

```
go run test/backend/echo/echo.go
```

* Start (fake) issuer server.

```
go run src/envoy/auth/sample/fake_issuer.go src/envoy/auth/sample/pubkey.jwk
```

* Then issue HTTP request to proxy.

With valid JWT:
```
token=`cat src/envoy/auth/sample/correct_jwt`
curl --header "Authorization: Bearer $token" http://localhost:9090/echo -d "hello world"
```

With invalid JWT:
```
token=`cat src/envoy/auth/sample/invalid_jwt`
curl --header "Authorization: Bearer $token" http://localhost:9090/echo -d "hello world"
```

## How it works

### How to receive JWT

Every HTTP request should contain a JWT in the HTTP Authorization header:
- `Authorization: Bearer <JWT>` 

### Behavior after verification

- If verification fails, the request will not be passed to the backend and the proxy will send a response with the status code 401 (Unauthorized) and the failure reason as message body.
- If verification succeeds, the request will be passed to the backend, together with an additional HTTP header:
  
  ```
  sec-istio-auth-userinfo: <UserInfo>
  ```
  
  Here, `<UserInfo>` is one of the following, which you can configure in Envoy config:
  
  - Payload JSON
  - base64url-encoded payload JSON
  - JWT without signature (= base64url-encoded header and payload JSONs)


## How to configure it

### Add this filter to the filter chain

In Envoy config,
```
"filters": [
  {
    "type": "decoder",
    "name": "jwt-auth",
    "config": <config>
  },
  ...
]
```

### Config format

Format of `<config>`:
```
{
 "userinfo_type": <type of user info>,
 "pubkey_cache_expiration_sec": <time in seconds to expire a cached public key>
 “issuers”:[
   {
     “name”: <issuer name>,
     "audiences": [
       <audience A>,
       <audience B>,
       ...
     ],
     “pubkey”: {
        “type”: <type>, 
        “uri”: <uri>, 
        "cluster": <name of cluster>,
        "file": <path of the file of public key>
        “value”: <raw string of public key>,
     }, 
   },
   ...
 ]
}
```

#### userinfo_type (string, optional)

It specifies what will be added in `sec-istio-auth-userinfo` HTTP header.
It should be one of the following:

- `payload` : payload JSON
- `payload_base64url` : base64url-encoded payload JSON
- `header_payload_base64url` : JWT without  signature

If not specified, the default value is `payload_base64url`.

#### [WARNING, This feature is under construction ([issue](https://github.com/istio/proxy/issues/468))] pubkey_cache_expiration_sec (number, optional)

It specifies how long a cached public key will be kept (in seconds).

If not specified, the default value is `600`.

#### issuers (array of object, required)

It specifies the issuers and their public keys.
You can register multiple issuers and 
every JWT will be considered to be valid if it's verified with one of these issuers.


For each issuer, the following informations are required:
- __name__  (string, required): issuer's name. It should be the same as the value of the `iss` claim of a JWT.
- __audiences__ (array of string, optional): 
It specifies the set of acceptable audiences.
If it is specified, JWT must have `aud` claim specified in this list.

- __pubkey__ (object, required): information about public key.
  `type` and (`value` or `file` or (`uri` and `cluster`)) are required.
  - __type__ (string, required): the format of public key. It should be one of {`"jwks"`, `"pem"`}.
  - __value__ (string, optional): string of public key.
  - __file__ (string, optional): path of the plain text file of the public key.
  - __uri__ (string, optional): URI of the public key. Note that in this case you should register the issuer as a cluster (as described below), and specify the name of the cluster.
  - __cluster__ (string, optional): cluster name of the issuer.
  
  __(WARNING:This feature is under construction ([issue](https://github.com/istio/proxy/issues/468)))__ When `uri` and `cluster` are given, this proxy fetches the public key and 
  updates every `pubkey_cache_expiration_sec` second. 


### Clusters

You should specify all servers which the Envoy proxy will send requests to, as clusters in Envoy config.
In particular, when the proxy needs to fetch a public key from an issuer server, 
it should be registered as a cluster.

Example:
```
"clusters": [
  {
    "name": "example_issuer",
    "connect_timeout_ms": 5000,
    "type": "strict_dns",
    "circuit_breakers": {
     "default": {
      "max_pending_requests": 10000,
      "max_requests": 10000
     }
    },
    "lb_type": "round_robin",
    "hosts": [
      {
        "url": "tcp://account.example.com:8080"
      }
    ]
  },
  ...
]
```