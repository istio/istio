The following instruction details the steps required to run E2E tests with a Vagrant VM on your local machine, so you can test and debug locally.

# Benefits:
1. Set up a local vagrant VM Environment once and run `make e2e_all E2E_ARGS="--use_local_cluster"` to run E2E tests from your development environment.
1. No need to worry about kubernetes cluster setup. The scripts take care of that.

# Prereqs:
1. Set up Istio Dev envrionment using https://github.com/istio/istio/wiki/Dev-Guide.

1. Install
  * [virtual box](https://www.virtualbox.org/wiki/Downloads) - Verify `virtualbox` command opens up a virtual box window
  * [docker](https://docs.docker.com/) - Verify `docker version` returns version >= 18.03.0-ce
  * [vagrant](https://www.vagrantup.com/downloads.html) - Verify `vagrant -v` returns version >= Vagrant 2.0.3
  * [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl) - Verify `kubectl version` returns both server and client versions
  * [curl](https://curl.haxx.se/)

You can run the following OS specific scripts to install all pre-requisites, or use them as a reference to install them manually. .

```bash
// assumes [homebrew](https://brew.sh) exists for Mac
sh prereqs.sh
```

# Steps
## 1. Set up Vagrant Environment
```bash
sh vm_setup.sh
```

## 2. Set up Docker daemon and kubectl on Host
```bash
sh host_setup.sh
```
If you are on macOS, you need to setup docker daemon using UI additionally.
Click on the docker icon and go into Preferences..., click into the Daemon tag.
Add `10.10.0.2:5000` to Insecure registries.
Finally click the `Apply and Start` button in the bottom to restart Docker with new setting.
The final setup should be like this:
![Docker Daemon on macOS](macos_docker_daemon.png)

## 3. Build istio images
Push images from your local dev environment to local registry on vagrant vm:
```bash
sh test_setup.sh
```
You should push new images whenever you modify istio source code.

## 4. Run tests!
### 1. Run tests on host machine
You can issue test command on your host machine.
E.g.
```bash
cd $ISTIO/istio
make e2e_simple E2E_ARGS="--use_local_cluster" HUB=10.10.0.2:5000 TAG=latest
```
You can keep repeating this step if you made any local changes and want to run e2e tests again.
Add E2E_ARGS="--use_local_cluster" to all your e2e tests as we are running tests on a local cluster.
### 2. Run tests on VM
You can also ssh to VM and issue test command within the VM.
E.g.
```bash
cd $ISTIO/istio/vagrant-kubernetes-istio
vagrant ssh
# Inside VM
cd $ISTIO/istio
make e2e_simple E2E_ARGS="--use_local_cluster"
```

### 3. Debug with Delve
You can try debugging Istio with debugger tool. vm_setup.sh already installs Delve into VM for us. To use Delve, we need process id of the binary we want to debug.
For example, if you want to debug a test, run that test in your host/vm.
```bash
# In the VM/Host
make e2e_simple E2E_ARGS="--use_local_cluster --skip_cleanup"
```
Now, find the pid of the process say pilot-discovery that you want to debug.
```bash
vagrant ssh
ps -ef | grep pilot-discovery
```
Then, you can run Delve
```bash
sudo -E env "PATH=$PATH" dlv attach <pid of pilot-discovery>
```
For more information, please check [Debug an Istio container with Delve](https://github.com/istio/istio/wiki/Dev-Guide#debug-an-istio-container-with-delve)

# Cleanup
To save the vagrant vm status:
```bash
vagrant halt
```

To destroy the vm:
```bash
vagrant destroy
``` 

To cleanup host settings only(restore kubectl and remove docker daemon setup)
```bash
sh host_cleanup.sh
```
If you are on macOS, please go to the Preferences->Daemon to remove `10.10.0.0:5000` from the "Insecure registries" section. Then apply changes and restart docker. 
