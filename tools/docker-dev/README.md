# Istio Development Environment in Docker

This `Dockerfile` creates an Ubuntu-based Docker image for developing on Istio.

## Image Configuration

* The base Istio development tools and the following additional tools are installed, with Bash completion configured:
  * [Docker CLI](https://docs.docker.com/engine/reference/commandline/cli/)
  * [Google Cloud SDK (gcloud)](https://cloud.google.com/sdk/gcloud/)
  * [kubectl](https://kubernetes.io/docs/reference/kubectl/kubectl/)
  * [Kubernetes IN Docker (KIND)](https://github.com/kubernetes-sigs/kind)
  * [Helm](https://helm.sh/)
* A user with the same name as the local host user is created. That user has full sudo rights without password.
* The following volumes are mounted from the host into the container to share development files and configuration:
  * Go directory: `$(GOPATH)` → `/home/$(USER)/go`
  * Google Cloud SDK config: `$(HOME)/.config/gcloud` → `/home/$(USER)/.config/gcloud`
  * Kubernetes config: `$(HOME)/.kube` → `/home/$(USER)/.kube`
  * Docker socket, to access Docker from within the container: `/var/run/docker.sock` → `/var/run/docker.sock`
* The working directory is `/home/$user/go/src/istio.io/istio`.

## Creating The Container

To create your dev container, run:

```
make dev-shell
```

The first time this target it run, a Docker image named `istio/dev:USER` is created, where USER is your local username.
Any subsequent run won't rebuild the image unless the `Dockerfile` is modified.

The first time this target is run, a container named `istio-dev` is run with this image, and an interactive shell is executed in the container.
Any subsequent run won't restart the container and will only start an additional interactive shell. 


## Kubernetes Cluster Creation Using KIND

A Kubernetes can be created using KIND. For instance, to create a cluster named `blah` with 2 workers, run the following command within the container:

```
export CLUSTER_NAME="blah"

kind create cluster --name="$CLUSTER_NAME" --config=- <<EOF
kind: Cluster
apiVersion: kind.sigs.k8s.io/v1alpha3
nodes:
- role: control-plane
- role: worker
- role: worker
EOF

export KUBECONFIG=$(kind get kubeconfig-path --name="$CLUSTER_NAME")
```

KIND was originally intended to run from the host, so KIND rewrites the kubeconfig to redirect the port to kubeadmin.
This rewriting must be undone to allow connecting directly from within the container:

```
docker exec "${CLUSTER_NAME}-control-plane" cat /etc/kubernetes/admin.conf > $KUBECONFIG
```

## Removing The Container

```
docker stop istio-dev
docker rm istio-dev
```