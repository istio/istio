# Integrating multi-cluster mesh with federated SPIRE servers

This sample deploys a setup of [Federated SPIRE Architecture](https://spiffe.io/docs/latest/architecture/federation/readme/)
as an example of integrating multi-cluster mesh with SPIRE and different trust domains.

## Before you begin

### Cluster

```shell
samples/kind-lb/setupkind.sh --cluster-name cluster-east --ip-space 254
samples/kind-lb/setupkind.sh --cluster-name cluster-west --ip-space 255
```

### Environment variables

```shell
export CTX_CLUSTER_EAST=$(kubectl config get-contexts -o name | grep kind-cluster-east)
export CTX_CLUSTER_WEST=$(kubectl config get-contexts -o name | grep kind-cluster-west)
```

## Demo

### Deploy SPIRE

1. Install Helm charts:

   ```shell
   helm repo add spiffe-hardened https://spiffe.github.io/helm-charts-hardened
   ```

1. Install SPIRE components (CRDs, CSI driver, server and agent):

   ```shell
   # CRDs
   helm --kube-context="${CTX_CLUSTER_EAST}" upgrade --install spire-crds spiffe-hardened/spire-crds --version 0.5.0
   helm --kube-context="${CTX_CLUSTER_WEST}" upgrade --install spire-crds spiffe-hardened/spire-crds --version 0.5.0
   # CSI driver, server and agent
   helm --kube-context="${CTX_CLUSTER_EAST}" upgrade --install spire spiffe-hardened/spire -n spire --create-namespace \
     -f samples/security/spire-trust-domain-federation/spire-base.yaml \
     -f samples/security/spire-trust-domain-federation/spire-east.yaml \
     --version 0.24.0 --wait
   helm --kube-context="${CTX_CLUSTER_WEST}" upgrade --install spire spiffe-hardened/spire -n spire --create-namespace \
     -f samples/security/spire-trust-domain-federation/spire-base.yaml \
     -f samples/security/spire-trust-domain-federation/spire-west.yaml \
     --version 0.24.0 --wait
   ```

1. Federate bundles:

   ```shell
   spire_bundle_endpoint_west=$(kubectl --context="${CTX_CLUSTER_WEST}" get svc spire-server -n spire -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
   west_bundle=$(kubectl --context="${CTX_CLUSTER_WEST}" exec -c spire-server -n spire --stdin spire-server-0  -- spire-server bundle show -format spiffe)
   indented_west_bundle=$(echo "$west_bundle" | jq -r '.' | sed 's/^/    /')
   (cat samples/security/spire-trust-domain-federation/cluster-federated-trust-domain.yaml; echo -e "  trustDomainBundle: |-\n$indented_west_bundle") |\
     sed "s/\${CLUSTER}/west/g" |\
     sed "s/\${BUNDLE_ENDPOINT}/$spire_bundle_endpoint_west/g" |\
     kubectl --context="${CTX_CLUSTER_EAST}" apply -f -
   ```

   ```shell
   spire_bundle_endpoint_east=$(kubectl --context="${CTX_CLUSTER_EAST}" get svc spire-server -n spire -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
   east_bundle=$(kubectl --context="${CTX_CLUSTER_EAST}" exec -c spire-server -n spire --stdin spire-server-0  -- spire-server bundle show -format spiffe)
   indented_east_bundle=$(echo "$east_bundle" | jq -r '.' | sed 's/^/    /')
   (cat samples/security/spire-trust-domain-federation/cluster-federated-trust-domain.yaml; echo -e "  trustDomainBundle: |-\n$indented_east_bundle") |\
     sed "s/\${CLUSTER}/east/g" |\
     sed "s/\${BUNDLE_ENDPOINT}/$spire_bundle_endpoint_east/g" |\
     kubectl --context="${CTX_CLUSTER_WEST}" apply -f -
   ```

### Deploy Istio

1. Install control planes:

   ```shell
   sed -e "s/\${LOCAL_CLUSTER}/east/g" \
     -e "s/\${REMOTE_CLUSTER}/west/g" \
     -e "s/\${REMOTE_BUNDLE_ENDPOINT}/$spire_bundle_endpoint_west/g" \
     samples/security/spire-trust-domain-federation/istio.tmpl | istioctl --context="${CTX_CLUSTER_EAST}" install -y -f -
   sed -e "s/\${LOCAL_CLUSTER}/west/g" \
     -e "s/\${REMOTE_CLUSTER}/east/g" \
     -e "s/\${REMOTE_BUNDLE_ENDPOINT}/$spire_bundle_endpoint_east/g" \
     samples/security/spire-trust-domain-federation/istio.tmpl | istioctl --context="${CTX_CLUSTER_WEST}" install -y -f -
   ```

1. Install an E/W gateway and expose services:

   ```shell
   samples/multicluster/gen-eastwest-gateway.sh --network west-network | istioctl --context="${CTX_CLUSTER_WEST}" install -y -f -
   kubectl --context="${CTX_CLUSTER_WEST}" patch deploy istio-eastwestgateway -n istio-system --type='merge' \
     -p '{"spec": {"template": {"metadata": {"annotations": {"inject.istio.io/templates": "gateway,spire-gateway"}}}}}'
   kubectl --context="${CTX_CLUSTER_WEST}" apply -n istio-system -f samples/multicluster/expose-services.yaml
   ```

1. Enable service discovery in remote cluster:

   ```shell
   WEST_API_SERVER_IP=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' cluster-west-control-plane)
   istioctl --context="${CTX_CLUSTER_WEST}" create-remote-secret --name=west --server="https://${WEST_API_SERVER_IP}:6443" | kubectl --context="${CTX_CLUSTER_EAST}" apply -f -
   ```

### Verify the installation

1. To confirm that Istiod is now able to communicate with the Kubernetes control plane of the remote cluster.

   ```shell
   istioctl --context="${CTX_CLUSTER_EAST}" remote-clusters
   ```

   ```text
   NAME     SECRET                                    STATUS     ISTIOD
   east                                               synced     istiod-8887cd9cd-zv2zf
   west     istio-system/istio-remote-secret-west     synced     istiod-8887cd9cd-zv2zf
   ```

   ```shell
   istioctl --context="${CTX_CLUSTER_WEST}" pc secret deploy/istio-eastwestgateway -n istio-system
   ```

   ```text
   RESOURCE NAME     TYPE           STATUS     VALID CERT     SERIAL NUMBER                        NOT AFTER                NOT BEFORE               TRUST DOMAIN
   default           Cert Chain     ACTIVE     true           508823656af07c0056bae33c3c9dd26f     2025-09-05T21:43:55Z     2025-09-05T17:43:45Z     west.local
   ROOTCA            CA             ACTIVE     true           bbe128cc1365e276cd5e114a482aefee     2025-09-06T17:33:34Z     2025-09-05T17:33:24Z     east.local
   ROOTCA            CA             ACTIVE     true           594e1e8df46d0e3f95ed3d4571abb011     2025-09-06T17:35:05Z     2025-09-05T17:34:55Z     west.local
   ```

1. Deploy the `HelloWorld` service:

   ```shell
   kubectl --context="${CTX_CLUSTER_EAST}" label namespace default istio-injection=enabled
   kubectl --context="${CTX_CLUSTER_WEST}" label namespace default istio-injection=enabled
   ```

   ```shell
   kubectl apply --context="${CTX_CLUSTER_EAST}" \
       -f samples/helloworld/helloworld.yaml \
       -l service=helloworld
   kubectl apply --context="${CTX_CLUSTER_WEST}" \
       -f samples/helloworld/helloworld.yaml \
       -l service=helloworld
   ```

1. Deploy V1 in the `east` cluster:

   ```shell
   kubectl apply --context="${CTX_CLUSTER_EAST}" \
       -f samples/helloworld/helloworld.yaml \
       -l version=v1
   kubectl --context="${CTX_CLUSTER_EAST}" patch deploy helloworld-v1 --type='merge' \
       -p '{"spec": {"template": {"metadata": {"annotations": {"inject.istio.io/templates": "sidecar,spire"}}}}}'
   ```

1. Deploy V2 in the `west` cluster:

   ```shell
   kubectl apply --context="${CTX_CLUSTER_WEST}" \
       -f samples/helloworld/helloworld.yaml \
       -l version=v2
   kubectl --context="${CTX_CLUSTER_WEST}" patch deploy helloworld-v2 --type='merge' \
       -p '{"spec": {"template": {"metadata": {"annotations": {"inject.istio.io/templates": "sidecar,spire"}}}}}'
   ```

1. Deploy `curl` in the `east` cluster:

   ```shell
   kubectl --context="${CTX_CLUSTER_EAST}" apply -f samples/curl/curl.yaml
   kubectl --context="${CTX_CLUSTER_EAST}" patch deploy curl --type='merge' \
       -p '{"spec": {"template": {"metadata": {"annotations": {"inject.istio.io/templates": "sidecar,spire"}}}}}'
   ```

1. Send a few requests and verify that you receive responses from `v1` and `v2`:

   ```shell
   kubectl --context="${CTX_CLUSTER_EAST}" exec deploy/curl -c curl -- curl -v helloworld:5000/hello
   ```

1. Verify logs - you should see that responses come from `v1` and `v2`:

   ```text
   Hello version: v1, instance: helloworld-v1-f957bfb54-fmqbn
   Hello version: v2, instance: helloworld-v2-5f86f7755b-6rhj4
   ...
   ```
