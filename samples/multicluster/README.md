# Instructions for building a multi-cluster multi-network mesh

Pick the mesh specific parameters.

```bash
# Organization name for root and intermediate certs.
export ORG_NAME=jason.example.com

# Pick a unique ID for the mesh.b
export MESH_ID=MyMesh
```

Create a working directory for generated certs and configuration.

```bash
export WORKDIR=mesh-workspace
[ ! -d "${WORKDIR}" ] && mkdir ${WORKDIR}
```

Prepare the initial configuration for the mesh. This creates a root key and cer
that will sign intermediate certs for each cluster.

```bash
./setup-mesh.sh create-mesh
```

dd your clusters to your mesh and apply configuration to build the multicluster

```bash
./setup-mesh.sh add-cluster ${KUBECONFIG0} ${CONTEXT0} ${NETWORK1}
./setup-mesh.sh add-cluster ${KUBECONFIG1} ${CONTEXT1} ${NETWORK1}
./setup-mesh.sh apply
```

Install the sample bookinfo application in each cluster.

```bash
./setup-bookinfo.sh install
```

Scale the bookinfo services in each cluster to simulate partial service
availability. The application should continue to function when accessed through
cluster's gateway.

```bash
# only serve review-v1 and ratings-v1
for DEPLOYMENT in details-v1 productpage-v1 reviews-v2 reviews-v3; do
    kubectl --kubeconfig=${CLUSTER0_KUBECONFIG} --context=${CLUSTER0_CONTEXT} \
        scale deployment ${DEPLOYMENT} --replicas=0
done

# only serve review-v2 and productpage-v1
for DEPLOYMENT in details-v1 reviews-v2 reviews-v3 ratings-v1; do
    kubectl --kubeconfig=${CLUSTER1_KUBECONFIG} --context=${CLUSTER1_CONTEXT} \
        scale deployment ${DEPLOYMENT} --replicas=0
done

# only serve review-v3 and details-v1
for DEPLOYMENT in productpage-v1 reviews-v2 reviews-v1 ratings-v1; do
    kubectl --kubeconfig=${CLUSTER2_KUBECONFIG} --context=${CLUSTER2_CONTEXT} \
        scale deployment ${DEPLOYMENT} --replicas=0
done
```

Teardown the mesh and remove the sample bookinfo application.

```bash
./setup-mesh.sh teardown
./setup-bookinfo.sh uninstall
```
