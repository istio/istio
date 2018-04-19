This sets up your local linux box to run tests on a kubernetes cluster on vagrant VM.

# Benefits:
1) Setup the vagrant VM Environment once and then using your normal make e2e_all commands from your development environment you can run tests on vagrant VM.
2) No need to worry about kubernetes cluster setup.. scripts will take care of it for you
3) TODO: Debug tests right from your development environment.

# Prereqs:
1) apt-get (should be available on linux, but make sure it's there on your mac box)
2) dpkg (should be available on linux, but make sure it's there on your mac box)
3) curl
4) [virtual box](https://www.virtualbox.org/wiki/Downloads)

   Verify `virtualbox` command opens up console for virtual box showing your vm's if any.
5) [docker-ce](https://docs.docker.com/install/linux/docker-ce/debian/#install-docker-ce-1)

   Verify `docker version` returns version >= 18.03.0-ce
6) [vagrant](https://www.vagrantup.com/downloads.html)

   Verify `vagrant -v` returns version >= Vagrant 2.0.3
7) [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl)

   Verify `kubectl version` returns versions for both server and client

For installation of curl, virtualbox, docker, vagrant and kubectl on linux, you can try following script:
```bash
sh setup_linux_prereqs.sh
```
Verify they are installed by running verification commands listed above for each of the components.

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

