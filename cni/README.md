[![Go Report Card](https://goreportcard.com/badge/github.com/istio/cni)](https://goreportcard.com/report/github.com/istio/cni)
[![GolangCI](https://golangci.com/badges/github.com/istio/cni.svg)](https://golangci.com/r/github.com/istio/cni)

# Istio CNI plugin

For application pods in the Istio service mesh, all traffic to/from the pods needs to go through the
sidecar proxies (istio-proxy containers).  This `istio-cni` Container Network Interface (CNI) plugin will
set up the pods' networking to fulfill this requirement in place of the current Istio injected pod `initContainers`
`istio-init` approach.

This is currently accomplished (for IPv4) via configuring the iptables rules in the netns for the pods.

The CNI handling the netns setup replaces the current Istio approach using a `NET_ADMIN` privileged
`initContainers` container, `istio-init`, injected in the pods along with `istio-proxy` sidecars.  This
removes the need for a privileged, `NET_ADMIN` container in the Istio users' application pods.

## Usage

A complete set of instructions on how to use and install the Istio CNI is available on the Istio documentation site under [Install Istio with the Istio CNI plugin](https://preliminary.istio.io/docs/setup/kubernetes/install/cni/).  Only a summary is provided here.  The steps are:

1. Install Kubernetes and `kubelet` in a manner that can support the CNI

1. Install Kubernetes with the [ServiceAccount admission controller](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#serviceaccount) enabled

1. Install the Istio CNI components. A specific example assuming locally built CNI images would be:

    ```console
    $ CNI_HUB=docker.io/my_userid
    $ CNI_TAG=mytag
    # run from the ${GOPATH}/src/istio.io/cni dir (repo where istio/cni was cloned)
    $ helm template --name=istio-cni --namespace=kube-system --set "excludeNamespaces={}" --set hub=${CNI_HUB} --set tag=${CNI_TAG} --set pullPolicy=IfNotPresent --set logLevel=debug  deployments/kubernetes/install/helm/istio-cni > istio-cni_install.yaml
    $ kubectl apply -f istio-cni_install.yaml
    ```

1. Create and apply Istio manifests with the Istio CNI plugin enabled using the `--set istio_cni.enabled=true` Helm variable

For most Kubernetes environments the `istio-cni` [helm parameters' defaults](deployments/kubernetes/install/helm/istio-cni/values.yaml) will configure the Istio CNI plugin in a manner compatible with the Kubernetes installation.  Refer to
the [Hosted Kubernetes Usage](#hosted-kubernetes-usage) section for Kubernetes environment specific procedures.

Helm chart parameters:

| Option | Values | Default | Description |
|--------|--------|---------|-------------|
| hub | | | The container registry to pull the `install-cni` image. |
| tag | | | The container tag to use to pull the `install-cni` image. |
| logLevel | `panic`, `fatal`, `error`, `warn`, `info`, `debug` | `warn` | Logging level for CNI binary |
| excludeNamespaces | `[]string` | `[ istio-system ]` | List of namespaces to exclude from Istio pod check |
| cniBinDir | | `/opt/cni/bin` | Must be the same as the environment's `--cni-bin-dir` setting (`kubelet` param) |
| cniConfDir | | `/etc/cni/net.d` | Must be the same as the environment's `--cni-conf-dir` setting (`kubelet` param) |
| cniConfFileName | | None | Leave unset to auto-find the first file in the `cni-conf-dir` (as `kubelet` does).  Primarily used for testing `install-cni` plugin config.  If set, `install-cni` will inject the plugin config into this file in the `cni-conf-dir` |
| psp_cluster_role | | | A `ClusterRole` that sets the according use of [PodSecurityPolicy](https://kubernetes.io/docs/concepts/policy/pod-security-policy) for the `ServiceAccount`|
| chained | `true` or `false` | `true` | Whether to deploy the config file as a plugin chain or as a standalone file in the conf dir. Some k8s flavors (e.g. OpenShift) do not support the chain approach, set to false if this is the case. |

### Hosted Kubernetes Usage

Not all hosted Kubernetes clusters are created with the `kubelet` configured to use the CNI plugin so
compatibility with this `istio-cni` solution is not ubiquitous.  The `istio-cni` plugin is expected
to work with any hosted kubernetes leveraging CNI plugins.  The below table indicates the known CNI status
of hosted Kubernetes environments and whether `istio-cni` has been trialed in the cluster type.

| Hosted Cluster Type | Uses CNI | istio-cni tested? |
|---------------------|----------|-------------------|
| GKE 1.9.7-gke.6 default | N | N |
| GKE 1.9.7-gke.6 w/ [network-policy](https://cloud.google.com/kubernetes-engine/docs/how-to/network-policy) | Y | Y |
| IKS (IBM cloud) | Y | Y (on k8s 1.10) |
| EKS (AWS) | Y | N |
| AKS (Azure) | Y | N |
| Red Hat OpenShift 3.10| Y | Y |

#### GKE Setup

1. Enable [network-policy](https://cloud.google.com/kubernetes-engine/docs/how-to/network-policy) in your cluster.  NOTE: for existing clusters this redeploys the nodes.

1. Make sure your kubectl user (service-account) has a ClusterRoleBinding to the `cluster-admin` role.  This is also a typical pre-requisite for installing Istio on GKE.
    1. `kubectl create clusterrolebinding cni-cluster-admin-binding --clusterrole=cluster-admin --user=istio-user@gmail.com`
        1. User `istio-user@gmail.com` is an admin user associated with the gcloud GKE cluster

1. Create the Istio CNI manifests with this Helm chart option `--set cniBinDir=/home/kubernetes/bin`

#### IKS Setup

No special set up is required for IKS, as it currently uses the default `cni-conf-dir` and `cni-bin-dir`.

#### Red Hat OpenShift Setup

Add the following section into [istio-cni.yaml](deployments/kubernetes/install/helm/istio-cni/templates/istio-cni.yaml#L109)
to run the `install-cni` DaemonSet container as privileged so that it has proper write permission in the host filesystem:

```yaml
securityContext:
  privileged: true
```

1. Grant privileged permission to `istio-cni` service account:

```console
$ oc adm policy add-scc-to-user privileged -z istio-cni -n kube-system
```

## Build

First, clone this repository under `$GOPATH/src/istio.io/`.

For linux targets:

```console
$ GOOS=linux make build
```

You can also build the project from a non-standard location like so:

```console
$ ISTIO_CNI_RELPATH=github.com/some/cni GOOS=linux make build
```

To push the Docker image:

```console
$ export HUB=docker.io/myuser
$ export TAG=dev
$ GOOS=linux make docker.push
```

**NOTE:** Set HUB and TAG per your docker registry.

### Helm

The Helm package tarfile can be created via

```console
$ helm package $GOPATH/src/istio.io/cni/deployments/kubernetes/install/helm/istio-cni
```

#### Serve Helm Repo

An example for hosting a test repo for the Helm istio-cni package:

1. Create package tarfile with `helm package $GOPATH/src/istio.io/cni/deployments/kubernetes/install/helm/istio-cni`
1. Copy tarfile to dir to serve the repo from
1. Run `helm serve --repo-path <dir where helm tarfile is> &`

    1. The repo URL will be output (`http://127.0.0.1:8879`)
    1. (optional) Use the `--address <IP>:<port>` option to bind the server to a specific address/port

To use this repo via `helm install`:

```console
$ helm repo add local_istio http://127.0.0.1:8879
$ helm repo update
```

At this point the `istio-cni` chart is ready for use by `helm install`.

To make use of the `istio-cni` chart from another chart:

1. Add the following to the other chart's `requirements.yaml`:

   ```yaml
   - name: istio-cni
     version: ">=0.0.1"
     repository: http://127.0.0.1:8879
     condition: istio-cni.enabled
   ```

1. Run `helm dependency update <chart>` on the chart that needs to depend on istio-cni.

    1. NOTE: for [istio/istio](https://github.com/istio/istio/tree/master/install/kubernetes/helm/istio) the charts
       need to be reorganized to make `helm dependency update` work.  The child charts (pilot, galley, etc) need to
       be made independent charts in the directorkefiy at the same level as the main `istio` chart
       (<https://github.com/istio/istio/pull/9306>).

## Testing

The Istio CNI testing strategy and execution details are explained [here](test/README.md).

## Troubleshooting

### Validate the iptables are modified

1. Collect your pod's container id using kubectl.

    ```console
    $ ns=test-istio
    $ podnm=reviews-v1-6b7f6db5c5-59jhf
    $ container_id=$(kubectl get pod -n ${ns} ${podnm} -o jsonpath="{.status.containerStatuses[?(@.name=='istio-proxy')].containerID}" | sed -n 's/docker:\/\/\(.*\)/\1/p')
    ```

1. SSH into the Kubernetes worker node that runs your pod.

1. Use `nsenter` to view the iptables.

    ```console
    $ cpid=$(docker inspect --format '{{ .State.Pid }}' $container_id)
    $ nsenter -t $cpid -n iptables -L -t nat -n -v --line-numbers -x
    ```

### Collecting Logs

The CNI plugins are executed by threads in the `kubelet` process.  The CNI plugins logs end up the syslog
under the `kubelet` process.  On systems with `journalctl` the following is an example command line
to view the last 1000 `kubelet` logs via the `less` utility to allow for `vi`-style searching:

```console
$ journalctl -t kubelet -n 1000 | less
```

#### GKE via Stackdriver Log Viewer

Each GKE cluster's will have many categories of logs collected by Stackdriver.  Logs can be monitored via
the project's [log viewer](https://cloud.google.com/logging/docs/view/overview) and/or the `gcloud logging read`
capability.

The following example grabs the last 10 `kubelet` logs containing the string "cmdAdd" in the log message.

```console
$ gcloud logging read "resource.type=gce_instance AND jsonPayload.SYSLOG_IDENTIFIER=kubelet AND jsonPayload.MESSAGE:cmdAdd" --limit 10 --format json
```

## Implementation Details

### Overview

- [istio-cni.yaml](deployments/kubernetes/install/helm/istio-cni/templates/istio-cni.yaml)
    - Helm chart manifest for deploying `install-cni` container as daemonset
    - `istio-cni-config` configmap with CNI plugin config to add to CNI plugin chained config
    - creates service-account `istio-cni` with `ClusterRoleBinding` to allow gets on pods' info

- `install-cni` container
    - copies `istio-cni` binary and `istio-iptables` to `/opt/cni/bin`
    - creates kubeconfig for the service account the pod runs under
    - injects the CNI plugin config to the config file pointed to by CNI_CONF_NAME env var
        - example: `CNI_CONF_NAME: 10-calico.conflist`
        - the program inserts `CNI_NETWORK_CONFIG` into the `plugins` list in `/etc/cni/net.d/${CNI_CONF_NAME}`

- `istio-cni`
    - CNI plugin executable copied to `/opt/cni/bin`
    - currently implemented for k8s only
    - on pod add, determines whether pod should have netns setup to redirect to Istio proxy
        - if so, calls `istio-iptables` with params to setup pod netns

- [istio-iptables](tools/istio-cni-docker.mk)
    - sets up iptables to redirect a list of ports to the port envoy will listen

### Background

The framework for this implementation of the CNI plugin is based on the
[containernetworking sample plugin](https://github.com/containernetworking/plugins/blob/master/plugins/sample).

#### Build Toolchains

The Istio makefiles and container build logic was leveraged heavily/lifted for this repo.

Specifically:
- golang build logic
- multi-arch target logic
- k8s lib versions (Gopkg.toml)
- docker container build logic
    - setup staging dir for docker build
    - grab built executables from target dir and cp to staging dir for docker build
    - tagging and push logic

#### Deployment

The details for the deployment & installation of this plugin were pretty much lifted directly from the
[Calico CNI plugin](https://github.com/projectcalico/cni-plugin).

Specifically:

- [CNI installation script](https://github.com/projectcalico/cni-plugin/blob/master/k8s-install/scripts/install-cni.sh)
    - This does the following
        - sets up CNI conf in /host/etc/cni/net.d/*
        - copies calico CNI binaries to /host/opt/cni/bin
        - builds kubeconfig for CNI plugin from service-account info mounted in the pod:
          <https://github.com/projectcalico/cni-plugin/blob/master/k8s-install/scripts/install-cni.sh#L142>
        - reference: <https://kubernetes.io/docs/reference/access-authn-authz/service-accounts-admin/>
- The CNI installation script is containerized and deployed as a daemonset in k8s.  The relevant
  calico k8s manifests were used as the model for the istio-cni plugin's manifest:
    - [daemonset and configmap](https://docs.projectcalico.org/v3.2/getting-started/kubernetes/installation/hosted/calico.yaml)
        - search for the `calico-node` Daemonset and its `install-cni` container deployment
    - [RBAC](https://docs.projectcalico.org/v3.2/getting-started/kubernetes/installation/rbac.yaml)
        - this creates the service account the CNI plugin is configured to use to access the kube-api-server

The installation program `install-cni` injects the `istio-cni` plugin config at the end of the CNI plugin chain
config.  It creates or modifies the file from the configmap created by the Kubernetes manifest.

#### Plugin Logic

##### cmdAdd

Workflow:
1. Check k8s pod namespace against exclusion list (plugin config)
    1. Config must exclude namespace that Istio control-plane is installed in
    1. If excluded, ignore the pod and return prevResult
1. Setup redirect rules for the pods:
    1. Get the port list from pods definition
    1. Setup iptables with required port list: `nsenter --net=<k8s pod netns> /opt/cni/bin/istio-iptables ...`

    Following conditions will prevent the redirect rules to be setup in the pods:

        1. Pods only have 1 container(no sidecar proxy injected)
        2. Pods have annotation `sidecar.istio.io/inject` set to `false` or has no key `sidecar.istio.io/status` in annotations
        3. Pod has `istio-init` initContainer
        4. Pods are in one of the namespaces specified in the `exclude_namespaces` parameter of the `istio-cni` plugin config
1.  Return prevResult

**TBD** istioctl / auto-sidecar-inject logic for handling things like specific include/exclude IPs and any
other features.
-  Watch configmaps or CRDs and update the `istio-cni` plugin's config
   with these options.

##### cmdDel

Anything needed?  The netns is destroyed by `kubelet` so ideally this is a NOOP.

##### Logging

The plugin leverages `logrus` & directly utilizes some Calico logging lib util functions.

## Comparison with Pod Network Controller Approach

The proposed [Istio pod network controller](https://github.com/sabre1041/istio-pod-network-controller) has
the problem of synchronizing the netns setup with the rest of the pod init.  This approach requires implementing
custom synchronization between the controller and pod initialization.

Kubernetes has already solved this problem by not starting any containers in new pods until the full CNI plugin
chain has completed successfully.  Also, architecturally, the CNI plugins are the components responsible for network
setup for container runtimes.
