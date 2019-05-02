# Benefits:
1. Set up a local kubeadm environment once and run E2E tests on local machine, so you can test and debug locally.
1. No need to worry about kubernetes cluster setup.

# Prereqs:
1. Set up Istio Dev environment using https://github.com/istio/istio/wiki/Preparing-for-Development.

1. Install
  * [docker](https://docs.docker.com/)
  * [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl) - Verify `kubectl version` returns both server and client versions
  * [curl](https://curl.haxx.se/) - Verify `curl --help` prints the help information.
  * [go](https://golang.org/doc/install)
  * [KinD](https://kind.sigs.k8s.io/)

You can run the following script to check/install of all pre-requisites, or use it as a reference to install them manually.

```bash
. ./install_prereqs.sh
```
# Steps
## 1. Set up KinD Environment
```bash
. ./setup_kind_env.sh
```

## 2. Build Istio Images
Build images on your host machine and load it into KinD's docker daemon.
```bash
. ./setup_test.sh
```

## 3. Run Test!
You can issue test commands on your host machine.
E.g.
```bash
cd $ISTIO/istio
make e2e_simple E2E_ARGS="--use_local_cluster" HUB=kind TAG=e2e
```

# Debug
To get the logs out from KinD:
```bash
kind export logs --name e2e [output_dir]
``` 


# Cleanup
To destroy the kubeadm:
```bash
kind delete cluster --name e2e
``` 