# Istioctl-proxy service

This sample implements a gRPC server that can be deployed on an external control plane's cluster to
intercept and aggregate (accross `istiod` instances) xDS requests made by `istioctl` CLI commands.
See [Configuring the istioctl CLI for a remote mesh cluster](https://istio.io/blog/2022/istioctl-proxy/).

To use it:

1. Install Istio by following the [external control plane install instructions](https://istio.io/latest/docs/setup/install/external-controlplane/).

1. Start the istioctl-proxy service on the external cluster:

    ```bash
    $ kubectl apply -n external-istiod -f istioctl-proxy.yaml --context="${CTX_EXTERNAL_CLUSTER}" 
    service/istioctl-proxy created
    serviceaccount/istioctl-proxy created
    secret/jwt-cert-key-secret created
    deployment.apps/istioctl-proxy created
    role.rbac.authorization.k8s.io/istioctl-proxy-role created
    rolebinding.rbac.authorization.k8s.io/istioctl-proxy-role created
    ```

1. Port-forward the proxy server port:

    ```bash
    kubectl port-forward -n external-istiod service/istioctl-proxy 9090:9090 --context="${CTX_EXTERNAL_CLUSTER}"
    ```

1. Configure `istioctl` to use the proxy:

    ```bash
    export ISTIOCTL_XDS_ADDRESS=localhost:9090
    export ISTIOCTL_ISTIONAMESPACE=external-istiod
    export ISTIOCTL_PREFER_EXPERIMENTAL=true
    ```

1. Try it out:

    ```bash
    $ istioctl x ps --context="${CTX_REMOTE_CLUSTER}" 
    NAME                                                      CDS        LDS        EDS        RDS        ISTIOD         VERSION
    helloworld-v1-776f57d5f6-tmpkd.sample                     SYNCED     SYNCED     SYNCED     SYNCED     <external>     1.12.1
    istio-ingressgateway-75bfd5668f-lggn4.external-istiod     SYNCED     SYNCED     SYNCED     SYNCED     <external>     1.12.1
    sleep-557747455f-v627d.sample                             SYNCED     SYNCED     SYNCED     SYNCED     <external>     1.12.1
    ```

    ```bash
    $ istioctl x ps helloworld-v1-776f57d5f6-tmpkd.sample --context="${CTX_REMOTE_CLUSTER}"
    Clusters Match
    Listeners Match
    Routes Match (RDS last loaded at Mon, 07 Mar 2022 14:42:59 EST)
    ```
