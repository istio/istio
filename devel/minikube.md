# Configuring development virtual machine to use a minikube cluster running on the same host

The following will help set-up and configure a development virtual machine to use a minikube virtual machine as a target cluster, with both virtual machines running side by side on the same host.

## Prerequisites:

1. Virtualbox installed on host (tested with Virtualbox versions 5.1.18-5.1.30 and 5.2.0)
1. an existing Linux development VM (tested with Xubuntu 16.04-17.10). The development machine will typically be already configured virtual network connection to the outside world, and requires a second virtual network interface to connect to minikube.
1. minikube and kubectl installed on the host (see, for example, [instructions here](https://kubernetes.io/docs/getting-started-guides/minikube/))

## Determine minikube network configuration

Start the minikube VM (`minikube start`) and wait for its initialization to complete. Once the minikube VM is running:

- use Virtualbox tools to note the host-only adapter used by minikube. Upon creation of a new VM, minikube typically creates a new host-only adapter, instead of reusing an existing one.
- determine the minikube machine's IP address, by running `minikube ip` (we'll refer to this as `$minikube-ip` later)

## Ensure the development and minikube machines are on the same network

Place the two virtual machines on the same host network. Configure the development VM with a second network card, connected to the same host-only adapter name as the minikube VM - don't change minikube's host-only adapter.

Set minikube machine's IP address using a static configuration. This avoids having development machine's minikube Kubernetes context from becoming stale on minikube restarts. Replace `$minikube-ip` with the output of `minikube ip` from the previous step:

```sh
minikube ssh "echo 'pkill udhcpc && ifconfig eth1 $minikube-ip netmask 255.255.255.0 broadcast 192.168.99.255 up' | sudo tee /var/lib/boot2docker/bootlocal.sh > /dev/null"
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
