
Set environment variables for the three clusters in the mesh

```
bash
export CLUSTER1_KUBECONFIG=${HOME}/.kube/config
export CLUSTER1_CONTEXT=west0

export CLUSTER0_KUBECONFIG=$HOME/.kube/config
export CLUSTER0_CONTEXT=west1

export CLUSTER2_KUBECONFIG=$HOME/.kube/config
export CLUSTER2_CONTEXT=east0
```

Configure an org name for root and intermediate certs.

```bash
export ORG_NAME=jason.example.com
```

Configure the MESH_ID.

```bash
export MESH_ID=MyMesh
```

Pick a working directory for generated files. This will be created if it doesn't
exist.

```bash
export WORKDIR=mesh-workspace
```

Create a single merged KUBECONFIG with only clusters in the mesh. This is used
in all subsequent steps. This only needs to be run once per mesh.

```bash
./update-kubeconfig.sh
```

Prepare the initial set of artifacts for the mesh. This creates the following:

* base control plane configuration
* initial empty mesh topology file
* root key and cert. This will sign all intermediate certs in the mesh.

This only needs to be run once per mesh.

```bash
./prepare-mesh.sh
```

Update the clusters after you've added your clusters to
`${WORKDIR}/topology. This script performs the following steps:

* generates interemediate certs for each cluster
* generates control plane configuration for each cluster. These are persisted as
  `${WORKDIR}/istio-<context>.yaml` for inspection.
* creates the necessary gateways for cross-network traffic.
* configures the control planes for cross-cluster service discovery.
* installs the bookinfo app in each cluster

```bash
./update-clusters.sh
```

You can simulate

```bash
# only serve review-v1 and ratings-v1
kubectl --context=us-west1-a_vpc0-prod0 scale deployment details-v1 --replicas=0
kubectl --context=us-west1-a_vpc0-prod0 scale deployment productpage-v1 --replicas=0
kubectl --context=us-west1-a_vpc0-prod0 scale deployment reviews-v2 --replicas=0
kubectl --context=us-west1-a_vpc0-prod0 scale deployment reviews-v3 --replicas=0

# only serve review-v2 and productpage-v1
kubectl --context=us-west1-a_vpc0-prod1 scale deployment ratings-v1 --replicas=0
kubectl --context=us-west1-a_vpc0-prod1 scale deployment details-v1 --replicas=0
kubectl --context=us-west1-a_vpc0-prod1 scale deployment reviews-v1 --replicas=0
kubectl --context=us-west1-a_vpc0-prod1 scale deployment reviews-v3 --replicas=0

# only serve review-v3 and details-v1
kubectl --context=us-east1-b_vpc1-prod0 scale deployment productpage-v1 --replicas=0
kubectl --context=us-east1-b_vpc1-prod0 scale deployment ratings-v1-v1 --replicas=0
kubectl --context=us-east1-b_vpc1-prod0 scale deployment reviews-v1 --replicas=0
kubectl --context=us-east1-b_vpc1-prod0 scale deployment reviews-v2 --replicas=0
```
