This sets up your local linux box to run tests on a kubernetes cluster on vagrant VM.

# Benefits:
1) Setup the vagrant VM Environment once and then using your normal make e2e_all commands from your development environment you can run tests on vagrant VM.
2) No need to worry about kubernetes cluster setup.. scripts will take care of it for you
3) TODO: Debug tests right from your development environment.

# Prereqs:
Following are the basic components required for local e2e testing:
1) [Docker](https://docs.docker.com/docker-for-mac/install/)
2) [VirtualBox](https://www.virtualbox.org/wiki/Downloads)
3) [Vagrant](https://www.vagrantup.com/downloads.html)
4) [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl)

## Prereqs Installation
### Linux
Please run following script, to check and install all the prereqs:
```bash
sh linux_prereqs.sh
```
### macOS
With [Homebrew](https://brew.sh) installed, you can install all the prerequisites in one step:
```bash
sh setup_macos_prerequs.sh
```

## Prereqs Verification
1) Docker
```bash
docker version
``` 
should show you the version information of docker(>= 18.03.0-ce).
### macOS
After this, double-click Docker.app in the Applications folder to start Docker.
The whale in the top status bar indicates that Docker is running, and accessible from a terminal.

2) Vagrant
```
vagrant -v
```
should show you the verison information of vagrant(>= Vagrant 2.0.3).

3) VirtualBox
```bash
virtualbox
``` 
should pop up the VirtualBox UI.

# Istio
Also you need to have Istio Dev Environment setup on your box!
Refer: https://github.com/istio/istio/wiki/Dev-Guide for that.

# Setup
## 1) Create a vagrant directory inside your istio repository.

```bash
cd $ISTIO/istio
mkdir -p vagrant
cd vagrant
```

## 2) Clone this repository in this folder

```bash
git clone https://github.com/JimmyCYJ/vagrant-kubernetes-istio.git
```

## 3) Setup Vagrant Environment
Temporary step below (this will be checked-in in istio repo and thus won't be needed afterwards):
```bash
sed -i 's/kube-registry.kube-system.svc.cluster.local/kube-registry/' $ISTIO/istio/tests/util/localregistry/localregistry.yaml 
```
Run the following commands to bring up and set up the vagrant vm
```bash
cd vagrant-kubernetes-istio/RunTestOnHost
sh startup.sh
```

## 4) Setup Docker daemon on Host
### On MacOS
Click on the docker icon and go into Preferences..., click into the Daemon tag.
Add `10.10.0.2:5000` to Insecure registries.
Finally click the `Apply and Start` button in the bottom to restart Docker with new setting.

### On Linux
Run the following script the complete the settings:
```bash
sh linux_docker_setup.sh
```

## 5) Now you are ready to run tests!

Push images from your local dev environment to local registry on vagrant vm:
```bash
cd $ISTIO/istio/vagrant/vagrant-kubernetes-istio/RunTestOnHost
sh test_setup.sh
```
After this you can run all the e2e tests using normal make commands. Ex:
```bash
cd $ISTIO/istio
make e2e_simple E2E_ARGS="--use_local_cluster"
```
You can keep repeating this step if you made any local changes and want to run e2e tests again.
Add E2E_ARGS="--use_local_cluster" to all your e2e tests as tests are we are running a local cluster.

**Steps 1,2 and 3 are supposed to be one time step unless you want to remove vagrant environment from your machine.**

# Cleanup
## 1) Cleanup test environment
```bash
cd $ISTIO/istio/vagrant/vagrant-kubernetes-istio/RunTestOnHost
vagrant halt
```

## 2) Cleanup vagrant environment
This is necessary if you want to remove vagrant VM setup from your host and want to bring it back to original state
```bash
cd $ISTIO/istio/vagrant/vagrant-kubernetes-istio/RunTestOnHost
sh cleanup_linux_host.sh
```

