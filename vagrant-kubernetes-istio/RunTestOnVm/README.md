
# Push Images from Host to VM and Run Test Command on VM

This sets up your local linux box to run tests on a kubernetes cluster on vagrant VM. 

# Prereqs:
The following tools are needed.
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

Please follow Istio developer guide to set up environment on both host machine and VM. [environment setup] (https://github.com/istio/istio/wiki/Dev-Guide#setting-up-environment-variables)

# Setup on Host Machine
1) Create a vagrant directory inside your istio repository.

```bash
source ~/.profile
cd $ISTIO
mkdir -p vagrant
cd vagrant
```

2) Clone this repository into $ISTIO/vagrant.

```bash
git clone https://github.com/JimmyCYJ/vagrant-kubernetes-istio.git
```

3) Setup Vagrant Environment
Please run the following command to set up vagrant environment. The script will prompt for sudo password.

```bash
cd $ISTIO/vagrant/vagrant-kubernetes-istio/RunTestOnVm
sh host_setup.sh
```

This is temporary step. It will be checked-in in istio repo and thus won't be needed afterwards:
```bash
sed -i 's/kube-registry.kube-system.svc.cluster.local/kube-registry/' $ISTIO/istio/tests/util/localregistry/localregistry.yaml 
```

Please run the following command to set up VM.
```bash
cd $ISTIO/vagrant/vagrant-kubernetes-istio/RunTestOnVm/
vagrant ssh

# These commands need to run in VM.
source ~/.profile
sh /vagrant-kubernetes-istio/RunTestOnVm/vm_setup.sh

# Make sure local registry is deployed successfully by running kubectl get pods -n kube-system."
kubectl get pods -n kube-system
exit
```

4) Now you are ready to run tests!
Make sure your VM is up and running. If not, you can run this command first.
```bash
cd $ISTIO/vagrant/vagrant-kubernetes-istio/RunTestOnVm/
vagrant up --provider virtualbox
```

Push images from your host machine to local registry on vagrant vm:
```bash
cd $ISTIO/vagrant/vagrant-kubernetes-istio/RunTestOnVm/
sh test_setup.sh
```
After this you can run all the e2e tests in the virtual machine. Ex:
```bash
cd $ISTIO/vagrant/vagrant-kubernetes-istio/RunTestOnVm/
vagrant ssh
# The following commands need to run inside VM.
cd $ISTIO/istio
make e2e_simple E2E_ARGS="--use_local_cluster"
```
You can keep repeating this step if you made any local changes and want to run e2e tests again.
Add E2E_ARGS="--use_local_cluster" to all your e2e tests as tests are we are running a local cluster.

**Steps 1,2 and 3 are supposed to be one time step unless you want to remove vagrant environment from your machine.**

# Debug with Delve
vm_setup.sh already installs Delve for us. To use Delve, we need process id of the binary we want to debug.
Assuem we have issue this command to run e2e_simple test.
```bash
make e2e_simple E2E_ARGS="--use_local_cluster --skip_cleanup"
```
For example, if we want to debug process pilot-discovery, we can find its pid by
```bash
ps -ef | grep pilot-discovery
```
Then, we can run Delve
```bash
sudo -E env "PATH=$PATH" dlv attach <pid of pilot-discovery>
```

For more information, please check [Debug an Istio container with Delve](https://github.com/istio/istio/wiki/Dev-Guide#debug-an-istio-container-with-delve)

# Cleanup
1) Cleanup test environment
```bash
cd $ISTIO/vagrant/vagrant-kubernetes-istio/RunTestOnVm/
vagrant halt
```

2) Cleanup vagrant environment
This is necessary if you want to remove vagrant VM setup from your host and want to bring it back to original state
```bash
cd $ISTIO/vagrant/vagrant-kubernetes-istio/RunTestOnVm/
sh cleanup_linux_host.sh
```
