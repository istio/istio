# Configuring development environment with minikube

The following recipes will help set-up and configure a development virtual machine to use a minikube as a target cluster.
Two alternatives are suggested:

1. a lightweight environment, with minikube running as docker container(s) inside the development VM
1. a more isolated environment with minikube running as a second virtual machines on the same host

## Assumptions

1. Virtualbox installed on host (tested with Virtualbox versions 5.1.18-5.1.30 and 5.2.0)
1. an existing Linux development VM (tested with Xubuntu 16.04-17.10)

## Lightweight minikube cluster using driver=none

This option creates a minikube cluster, using docker, running on the same machine as the development.
Requires minikube .

### Prerequisites

1. Download and install a compatible kubectl (matches k8s and Istio requirements) and minikube (release 0.19+ to support local, hypervisor free, execution).

```sh
# download and install kubectl ...
curl -Lo kubectl https://storage.googleapis.com/kubernetes-release/release/v1.7.4/bin/linux/amd64/kubectl \
    && chmod +x kubectl && sudo mv kubectl /usr/local/bin/
# ... and minikube
curl -Lo minikube https://storage.googleapis.com/minikube/releases/v0.22.3/minikube-linux-amd64 \
    && chmod +x minikube && sudo mv minikube /usr/local/bin/
```

### Start the minikube cluster

1. Start the minikube cluster with required options (k8s version, API master extensions, etc. Note the use of *--vm-driver=none*)

```sh
# start minikube ...
sudo -E minikube start \
    --extra-config=apiserver.Admission.PluginNames="Initializers,NamespaceLifecycle,LimitRanger,ServiceAccount,DefaultStorageClass,GenericAdmissionWebhook,ResourceQuota" \
    --kubernetes-version=v1.7.5 --vm-driver=none
# set the kubectl context to minikube (alternative: kubectl config use-context minikube)
sudo -E minikube update-context
# wait for the cluster to become ready/accessible via kubectl
JSONPATH='{range .items[*]}{@.metadata.name}:{range @.status.conditions[*]}{@.type}={@.status};{end}{end}'; \
    until sudo kubectl get nodes -o jsonpath="$JSONPATH" 2>&1 | grep -q "Ready=True"; do sleep 1; done
sudo -E kubectl cluster-info
```

### Terminate minikube cluster

As of version 0.23, since minikube uses the host's docker daemon, it may leave "orphaned" containers. These are still present on the host. Future minikube versions may perform correct cleanup on exit. As a workaround, you may terminate all minikube spawned containers using the following commands (possibly added as aliases to `~/.bashrc`).

```sh
alias minikube-kill = `docker rm $(docker kill $(docker ps -a --filter="name=k8s_" --format="{{.ID}}"))`
alias minikube-stop = `docker stop $(docker ps -a --filter="name=k8s_" --format="{{.ID}}")`
```

The above assumes that *all* and *only* containers created by minikube have a name prefixed with `k8s_`.

## Separate minikube cluster VM

### Prerequisite

1. The development machine will typically be already configured virtual network connection to the outside world, and requires a second virtual network interface to connect to minikube.
1. minikube and kubectl installed on the host (see, for example, [instructions here](https://kubernetes.io/docs/getting-started-guides/minikube/))
1. Download and install a compatible kubectl (matches k8s and Istio requirements) and minikube (any recent release, tested with 0.18+).

```sh
# download and install kubectl ...
curl -Lo kubectl https://storage.googleapis.com/kubernetes-release/release/v1.7.4/bin/linux/amd64/kubectl \
    && chmod +x kubectl && sudo mv kubectl /usr/local/bin/
# ... and minikube
curl -Lo minikube https://storage.googleapis.com/minikube/releases/v0.22.3/minikube-linux-amd64 \
    && chmod +x minikube && sudo mv minikube /usr/local/bin/
```

### Start minikube VM

Start minikube from the host machine's command line and wait for its initialization to complete:

```sh
# start minkube, optionally passing in --driver=virtualbox
minikube start \
    --extra-config=apiserver.Admission.PluginNames="Initializers,NamespaceLifecycle,LimitRanger,ServiceAccount,DefaultStorageClass,GenericAdmissionWebhook,ResourceQuota" \
    --kubernetes-version=v1.7.5
```

### Determine minikube network configuration

Once the minikube VM is running:

- use Virtualbox tools to note the host-only adapter used by minikube. Upon creation of a new VM, minikube typically creates a new host-only adapter, instead of reusing an existing one.
- determine the minikube machine's IP address, by running `minikube ip` (we'll refer to this as `$minikube-ip` later)

### Ensure the development and minikube machines are on the same network

Place the two virtual machines on the same host network. Configure the development VM with a second network card, connected to the same host-only adapter name as the minikube VM - don't change minikube's host-only adapter.

Set minikube machine's IP address using a static configuration. This avoids having development machine's minikube Kubernetes context from becoming stale on minikube restarts. Replace `$minikube-ip` with the output of `minikube ip` from the previous step:

```sh
minikube ssh "echo 'pkill udhcpc && ifconfig eth1 $minikube-ip netmask 255.255.255.0 broadcast 192.168.99.255 up' \
    | sudo tee /var/lib/boot2docker/bootlocal.sh > /dev/null"
```

_If not using minikube's default CIDR (`192.168.99.1/24`), be sure to replace the netmask and broadcast address accordingly._

**To retain the static IP configuration, always start minikube through the command line (`minikube start`) and not through the Virtualbox management GUI.**

### Configure kubectl inside development machine to access minikube cluster

Run the following, on the _host_, to determine the user access token to minikube cluster.

```sh
kubectl describe secrets
Name:           default-token-xj982
...
token:          eyJhb...<redacted>...
```

Copy the token's value as `$minikube-token` and then proceed to configure `kubectl`, in the _development machine_, to use the minikube cluster:

```sh
kubectl config set-cluster minikube -server=https://$minikube-ip:8443 --insecure-skip-tls-verify=true
kubectl config set-credentials minikube --token=$minikube-token
kubectl config set-context minikube --cluster=minikube --user=minikube
kubectl config use-context minikube
```

Run `kubectl get nodes` inside the development virtual machine to confirm the configuration has been set correctly.
