# Benefits:
1. Set up a local minikube VM Environment once and run E2E tests on local machine, so you can test and debug locally.
1. No need to worry about kubernetes cluster setup.

# Prereqs:
1. Set up Istio Dev environment using https://github.com/istio/istio/wiki/Dev-Guide.

1. Install
  * [kvm2 for linux](https://www.linux-kvm.org/page/Main_Page) 
  * [hyperkit for mac](https://github.com/kubernetes/minikube/blob/master/docs/drivers.md#hyperkit-driver)
  * [docker](https://docs.docker.com/) - Verify `docker version` returns version >= 18.03.0-ce
  * [minikube](https://kubernetes.io/docs/tasks/tools/install-minikube/) - Verify `minikube version` returns version >= minikube 0.27.0
  * [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl) - Verify `kubectl version` returns both server and client versions
  * [curl](https://curl.haxx.se/) - Verify `curl --help` prints the help information.

You can run the following script to check/install of all pre-requisites, or use it as a reference to install them manually.
(This requires installation of [Homebrew](https://brew.sh) on macOS or debian based Linux distributions)

```bash
. ./install_prereqs.sh
```

# Steps
## 1. Set up Minikube Environment
```bash
. ./setup_host.sh
```

We support customize minikube `--vm-driver`, the default is kvm2 you can set any vm-driver you like via exporting `VM_DRIVER`for you environment.

For VM that doesn't support nested virtualization, you may pass `--vm-driver=none` via `export VM_DRIVER=none`.

## 2. Build istio images
Build images on your host machine:
```bash
. ./setup_test.sh
```
Minikube reuses the Docker daemon on your host machine to access images.

## 2. Run tests!
You can issue test commands on your host machine.
E.g.
```bash
cd $ISTIO/istio
make e2e_simple E2E_ARGS="--use_local_cluster" HUB=localhost:5000 TAG=e2e
```
Note the special arguments like **E2E_ARGS**, **HUB**, and **TAG**. They are required to run these tests with the local cluster and a local registry inside the VM. And you can run multiple E2E tests sequentially against the same VM.
The script has a number of options available [here](../../README.md#options-for-e2e-tests)

# Cleanup
To save the minikube status:
```bash
minikube stop
```

To destroy the minikube:
```bash
minikube delete
``` 

To cleanup host settings only (remove docker daemon setup and port forwarding)
```bash
. ./cleanup_host.sh
```
### Debug with KubeSquash
You can try debugging Istio with debugger tool [KubeSquash](https://github.com/solo-io/kubesquash). 
For example, if you want to debug discovery container in pilot, follow steps as follows:
1. Run that test in your host/vm.
   ```bash
   # In the VM/Host
   make e2e_simple E2E_ARGS="--use_local_cluster --skip_cleanup" HUB=10.10.0.2:5000 TAG=e2e
   ```
1. Run the kubesquash binary
1. Select the namespace of istio mesh: istio-system
1. Select Pilot Pod from the pods list
1. Select discovery container after that
1. You will get a prompt like `Going to attach dlv to pod istio-pilot-67db57c96d-c86ff. continue? `. Say Yes

After this delve would be attached to discovery container for you to debug.

For more information on debugging with delve, please check [Debug an Istio container with Delve](https://github.com/istio/istio/wiki/Dev-Guide#debug-an-istio-container-with-delve)

# Troubleshooting
Please refer [Troubleshooting](Troubleshooting.md) doc for information on this.

# Tips
Please refer [Tips](../Tips.md) doc for some suggestions that we have found useful for debugging with e2e tests.
