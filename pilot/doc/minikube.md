# Instructions on how to develop on mac with minikube

## Preparation

1. Install [minikube v0.19](https://github.com/kubernetes/minikube)

2. Install [xhyve driver](https://github.com/kubernetes/minikube/blob/master/docs/drivers.md#xhyve-driver).

       brew install docker-machine-driver-xhyve
       sudo chown root:wheel $(brew --prefix)/opt/docker-machine-driver-xhyve/bin/docker-machine-driver-xhyve
       sudo chmod u+s $(brew --prefix)/opt/docker-machine-driver-xhyve/bin/docker-machine-driver-xhyve

3. Start minikube:

       minikube start --vm-driver xhyve

_Note_: make sure to allow network communication from the virtual machine by
disabling firewall restrictions.  If you use wireless network, you might need
to restart the VM after sleep since the network can be flaky. The error
manifests in inability to pull images.

## Build and test

1. Mount repository root to minikube host folder:

       # In the repository root directory; the daemon must stay alive for the duration of the build/test
       minikube mount .:/pilot

2. Start the build pod:

       kubectl apply -f doc/bazel.yaml

3. Run bazel build and test

       kubectl exec bazel -it bash
       cd /pilot

       # make sure platform/kube/config exists; touch an empty file to use the minikube cluster for testing
       # Symlinks to ${HOME}/.kube/config will not work.
       sudo touch platform/kube/config

       sudo -E bazel test //... --symlink_prefix=/ --test_env KUBERNETES_SERVICE_PORT --test_env KUBERNETES_SERVICE_HOST

_Note_: the base image runs under user `jenkins` while the shared directories
are owned by the root; hence, the need to use `sudo` and passing environment
with `-E` flag.
