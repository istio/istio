# Multicluster Samples

The samples in this directory help to support multicluster use cases for the
following configurations:

* Primary-Remote
* Multinetwork

All of these instructions here assume that Istio has already been deployed to your primary clusters.

## Creating East-West Gateway

All configurations rely on a separate gateway deployment that is dedicated to
east-west traffic. This is done to avoid having east-west traffic flooding
the default north-south ingress gateway.

Run the following command to deploy the east-west gateway to a primary cluster:

```bash
export MESH=mesh1
export CLUSTER=cluster1
export NETWORK=network1
./samples/multicluster/gen-eastwest-gateway.sh | \
    istioctl manifest generate -f - | \
    kubectl apply -f -
```

The `CLUSTER` and `NETWORK` environment variables should match the values used to deploy the control plane
in that cluster.

## Primary-Remote Configuration

In order to give a remote cluster access to the control plane (istiod) in a primary cluster,
we need to expose the istiod service through the east-west gateway:

```bash
kubectl apply -f samples/multicluster/expose-istiod.yaml -n istio-system
```

## Multi-network Configuration

In order to enable cross-cluster load balancing between clusters that are in different
networks, we need to expose the services through the east-west gateway in each cluster:

 ```bash
 kubectl apply -f samples/multicluster/expose-services.yaml -n istio-system
 ```
