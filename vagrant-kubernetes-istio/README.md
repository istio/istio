# vagrant-kubernetes-istio

Set up Kubernetes on VM with vagrant for Istio e2e tests, so that we can run Istio e2e tests on local machine [issue](https://github.com/istio/istio/issues/4536)

# [RunTestOnHost](https://github.com/istio/istio/tree/vagrant-kubernetes-istio/vagrant-kubernetes-istio/RunTestOnHost "RunTestOnHost")
This test environment is tested on Ubuntu 16.04 (host), virtualbox 5.2.8, docker 17.09.0-ce, kubernetes 1.10.
 - Build Istio docker images on host machine.
 - Set up private docker registry in Kubernetes on VM.
 - Push images from host to VM. The images are stored in private docker registry
 - Run Istio e2e tests command on Host. The test environment is deployed in Kubernetes on VM, and test result is returned to host.


# [RunTestOnVm](https://github.com/istio/istio/tree/vagrant-kubernetes-istio/vagrant-kubernetes-istio/RunTestOnVm "RunTestOnVm")
This test environment is tested on Ubuntu 16.04 (host), virtualbox 5.2.8, docker 17.09.0-ce, kubernetes 1.10.
 - Build Istio docker images on host machine.
 - Set up private docker registry in Kubernetes on VM.
 - Push images from host to VM. The images are stored in private docker registry
 - Run Istio e2e tests command on VM. The test result is returned to VM.

