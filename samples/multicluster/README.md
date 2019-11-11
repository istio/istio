# Instructions for building a multi-cluster multi-network mesh

Choose clusters to build a mesh from. The clusters may be on the same or
diferent network.

```bash
export CLUSTER0_KUBECONFIG=${HOME}/.kube/config
export CLUSTER0_CONTEXT=west0

export CLUSTER1_KUBECONFIG=$HOME/.kube/config
export CLUSTER1_CONTEXT=west1

export CLUSTER2_KUBECONFIG=$HOME/.kube/config
export CLUSTER2_CONTEXT=east0
```

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

Create a single merged KUBECONFIG with clusters in the mesh. This is used in
subsequent steps.

```bash
./setup-mesh.sh prepare-kubeconfig
```

Prepare the initial configuration for the mesh. This creates:

* The base control plane configuration.
* The initial empty mesh topology file that you will add your clusters to.
* A root key and cer that will sign intermediate certs for each cluster.

```bash
./setup-mesh.sh prepare-mesh
```

Build the mesh. Add your clusters to `${WORKDIR}/topology` and apply
configuration to build the multicluster

```bash
# add your clusters following the example in the file.
${EDITOR} ${WORKDIR}/topology.yaml

#
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
