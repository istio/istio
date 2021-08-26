# Istio CNI plugin

For application pods in the Istio service mesh, all traffic to/from the pods needs to go through the
sidecar proxies (istio-proxy containers).  This `istio-cni` Container Network Interface (CNI) plugin will
set up the pods' networking to fulfill this requirement in place of the current Istio injected pod `initContainers`
`istio-init` approach.

This is currently accomplished via configuring the iptables rules in the netns for the pods.

The CNI handling the netns setup replaces the current Istio approach using a `NET_ADMIN` privileged
`initContainers` container, `istio-init`, injected in the pods along with `istio-proxy` sidecars.  This
removes the need for a privileged, `NET_ADMIN` container in the Istio users' application pods.

## Usage

A complete set of instructions on how to use and install the Istio CNI is available on the Istio documentation site under [Install Istio with the Istio CNI plugin](https://istio.io/latest/docs/setup/additional-setup/cni/).

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
under the `kubelet` process. On systems with `journalctl` the following is an example command line
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
$ gcloud logging read "resource.type=k8s_node AND jsonPayload.SYSLOG_IDENTIFIER=kubelet AND jsonPayload.MESSAGE:cmdAdd" --limit 10 --format json
```

## Implementation Details

### Overview

- [istio-cni Helm chart](../manifests/charts/istio-cni/templates)
    - `install-cni` daemonset
    - `istio-cni-config` configmap with CNI plugin config to add to CNI plugin chained config
    - creates service-account `istio-cni` with `ClusterRoleBinding` to allow gets on pods' info

- `install-cni` container
    - copies `istio-cni`, `istio-iptables` and `istio-cni-taint` to `/opt/cni/bin`
    - creates kubeconfig for the service account the pod runs under
    - injects the CNI plugin config to the CNI config file
        - CNI installer will try to look for the config file under the mounted CNI net dir based on file name extensions (`.conf`, `.conflist`)
        - the file name can be explicitly set by `CNI_CONF_NAME` env var
        - the program inserts `CNI_NETWORK_CONFIG` into the `plugins` list in `/etc/cni/net.d/${CNI_CONF_NAME}`

- `istio-cni`
    - CNI plugin executable copied to `/opt/cni/bin`
    - currently implemented for k8s only
    - on pod add, determines whether pod should have netns setup to redirect to Istio proxy. See [cmdAdd](#cmdadd-workflow) for detailed logic.
        - If so, calls `istio-iptables` with params to setup pod netns

- `istio-iptables`
    - sets up iptables to redirect a list of ports to the port envoy will listen

### CmdAdd Workflow

`CmdAdd` is triggered when there is a new pod created.

1. Check k8s pod namespace against exclusion list (plugin config)
    1. Config must exclude namespace that Istio control-plane is installed in
    1. If excluded, ignore the pod and return prevResult
1. Setup redirect rules for the pods:
    1. Get the port list from pods definition
    1. Setup iptables with required port list: `nsenter --net=<k8s pod netns> /opt/cni/bin/istio-iptables ...`. Following conditions will prevent the redirect rules to be setup in the pods:
        1. Pods have annotation `sidecar.istio.io/inject` set to `false` or has no key `sidecar.istio.io/status` in annotations
        1. Pod has `istio-init` initContainer
1. Return prevResult

## Reference

The framework for this implementation of the CNI plugin is based on the
[containernetworking sample plugin](https://github.com/containernetworking/plugins/tree/master/plugins/sample)

The details for the deployment & installation of this plugin were pretty much lifted directly from the
[Calico CNI plugin](https://github.com/projectcalico/cni-plugin).

Specifically:

- The CNI installation script is containerized and deployed as a daemonset in k8s.  The relevant
  calico k8s manifests were used as the model for the istio-cni plugin's manifest:
    - [daemonset and configmap](https://docs.projectcalico.org/v3.2/getting-started/kubernetes/installation/hosted/calico.yaml)
        - search for the `calico-node` Daemonset and its `install-cni` container deployment
    - [RBAC](https://docs.projectcalico.org/v3.2/getting-started/kubernetes/installation/rbac.yaml)
        - this creates the service account the CNI plugin is configured to use to access the kube-api-server

The installation program `install-cni` injects the `istio-cni` plugin config at the end of the CNI plugin chain
config.  It creates or modifies the file from the configmap created by the Kubernetes manifest.

## TODO

- Watch configmaps or CRDs and update the `istio-cni` plugin's config with these options.
