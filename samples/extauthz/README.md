# Ext Authz Service

[Ext Authz server](src/) implements the external server for the [Envoy ext_authz filter](https://www.envoyproxy.io/docs/envoy/v1.16.0/intro/arch_overview/security/ext_authz_filter)
as an example of integrating custom authorization system into Istio.

The Ext Authz server supports authorization check request using either HTTP (port 8000) or gRPC v2/v3 (port 9000) API and
will allow the request if it includes the header `x-ext-authz: allow` or if the service account of the source workload is `a`.
Note that `a` is just a default value for testing. It can be changed with the flag `-allow_service_account` when running the ext authz server.

## Usage

1. Deploy the Ext Authz service in a dedicated pod:

    ```console
    $ kubectl apply -f ext-authz.yaml
    service/ext-authz created
    deployment.extensions/ext-authz created
    ```

    Note, you can also deploy the Ext Authz service locally with the application container in the same pod, see the example in `local-ext-authz.yaml`.

1. Verify the Ext Authz server is up and running:

    Deploy a sleep pod to send the request:

    ```console
    $ kubectl apply -f ../sleep/sleep.yaml
    ```

    Send a check request with header `x-ext-authz: allow` to the Ext Authz server:

    ```console
    $ kubectl exec -it $(kubectl get pod -l app=sleep -n foo -o jsonpath={.items..metadata.name}) -c sleep -- curl -v ext-authz:8000 -H "x-ext-authz: allow"
       *   Trying 10.97.88.183:8000...
       * Connected to ext-authz-server (10.97.88.183) port 8000 (#0)
       > GET / HTTP/1.1
       > Host: ext-authz-server:8000
       > User-Agent: curl/7.73.0-DEV
       > Accept: */*
       > x-ext-authz: allow
       >
       * Mark bundle as not supporting multiuse
       < HTTP/1.1 200 OK
       < x-ext-authz-result: allowed
       < date: Tue, 03 Nov 2020 03:06:11 GMT
       < content-length: 0
       < x-envoy-upstream-service-time: 19
       < server: envoy
       <
       * Connection #0 to host ext-authz-server left intact
    ```

    As you observe, the check request with header `x-ext-authz: allow` is allowed by the Ext Authz server.

    Send another check request with `x-ext-authz: blabla` to the Ext Authz server:

    ```console
    $ kubectl exec -it $(kubectl get pod -l app=sleep -n foo -o jsonpath={.items..metadata.name}) -c sleep -- curl -v ext-authz:8000 -H "x-ext-authz: bla"
        > GET / HTTP/1.1
        > Host: ext-authz-server:8000
        > User-Agent: curl/7.73.0-DEV
        > Accept: */*
        > x-ext-authz: allowx
        >
        * Mark bundle as not supporting multiuse
        < HTTP/1.1 403 Forbidden
        < x-ext-authz-check-result: denied
        < date: Tue, 03 Nov 2020 03:14:02 GMT
        < content-length: 76
        < content-type: text/plain; charset=utf-8
        < x-envoy-upstream-service-time: 44
        < server: envoy
        <
        * Connection #0 to host ext-authz-server left intact
        denied by ext_authz for not found header `x-ext-authz: allow` in the request
    ```

    As you observe, the check request with header `x-ext-authz: bla` is denied by the Ext Authz server.

1. To clean up, execute the following commands:

    ```console
    $ kubectl delete -f ../sleep/sleep.yaml
    $ kubectl delete -f ext-authz.yaml
    ```
