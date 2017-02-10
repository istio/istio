# Istio Manager #
[![Build Status](https://travis-ci.org/istio/manager.svg?branch=master)](https://travis-ci.org/istio/manager)
[![Go Report Card](https://goreportcard.com/badge/github.com/istio/manager)](https://goreportcard.com/report/github.com/istio/manager)
[![GoDoc](https://godoc.org/github.com/istio/manager?status.svg)](https://godoc.org/github.com/istio/manager)

The Istio Manager is used to configure Istio and propagate configuration to the
other components of the system, including the Istio mixer and the Istio proxy mesh.

[Contributing to the project](./CONTRIBUTING.md)

## Filing issues ##

If you have a question about the Istio Manager or have a problem using it, please
[file an issue](https://github.com/istio/manager/issues/new).

## Build instructions for Linux and Mac ##

We are using [Bazel 0.4.4](https://bazel.io) to build Istio Manager:

    bazel build //cmd/manager

Bazel uses `BUILD` files to keep track of dependencies between sources.  If you
add a new source file or change the imports, please run the following command
to update all `BUILD` files:

    gazelle -go_prefix "istio.io/manager" --mode fix -repo_root .

Gazelle binary is located in `bazel-bin/external` folder under the manager
repository, after your initial bazel build:

    bazel-bin/external/io_bazel_rules_go_repository_tools/bin/gazelle

_Note_: If you cant find the gazelle binary in the path mentioned above,
try to update the mlocate database and run `locate gazelle`. The gazelle
binary should typically be in

    $HOME/.cache/bazel/_bazel_<username>/<somelongfoldername>/external/io_bazel_rules_go_repository_tools/bin/gazelle

## Test environment ##

Manager tests require access to a Kubernetes cluster (version 1.5.2 or higher). Each
test operates on a temporary namespace and deletes it on completion.  Please
configure your `kubectl` to point to a development cluster (e.g. minikube)
before building or invoking the tests and add a symbolic link to your
repository pointing to Kubernetes cluster credentials:

    ln -s ~/.kube/config platform/kube/

_Note1_: If you are running Bazel in a Vagrant VM (as described below), copy
the kube config file on the host to platform/kube instead of symlinking it,
and change the paths to minikube certs.

    cp ~/.kube/config platform/kube/
    sed -i 's!/Users/<username>!/home/ubuntu!' platform/kube/config

Also, copy the same file to `/home/ubuntu/.kube/config` in the VM, and make
sure that the file is readable to user `ubuntu`.

_Note2_: If you are using GKE, please make sure you are using static client
certificates before fetching cluster credentials:

    gcloud config set container/use_client_certificate True

To run the tests:

    bazel test //...

## Docker images (Linux-only) ##

We provide Bazel targets to output Istio runtime images:

    bazel run //docker:runtime
    
The image includes Istio Proxy and Istio Manager.

For interactive debug and development it is often useful to 'exec' into the
application pod, modify iptable rules, ping, curl, etc. For this purpose the
prebuilt *docker.io/istio/debug:test* image has been created. To use this image
add the following snippet to your deployment spec alongside the proxy and app
containers.

    - name: debug
      image: docker.io/istio/debug:test
      securityContext:
          privileged: true

Use `kubectl exec` to access the debug container.

    kubectl exec -it <pod-name> bash -c debug

The proxy injection process redirects *all* inbound and outbound traffic through
the proxy via iptables. This can sometimes be undesirable while debugging, e.g.
trying to install additional test tools via apt-get. Use
`proxy-redirection-clear` to temporarily disable the iptable redirection rules
and `proxy-redirection-restore` to restore them.

## Vagrant build environment

_Note:_ This section applies to Mac and Windows users only.

### Pre-requisites ##

- Setup Go 1.7.5+ on your host machine
- Clone this repository
- Install [Virtualbox](https://github.com/kubernetes/minikube/releases)
- Install [Minikube](https://github.com/kubernetes/minikube/releases)
- Install [Vagrant](https://www.vagrantup.com/downloads.html)
- Install [kubectl](https://kubernetes.io/docs/user-guide/prereqs/)

### 1. Start Minikube

Istio manager needs kubernetes versions 1.5.2 or higher.

    minikube config set kubernetes-version v1.5.2
    minikube start
    
Copy the kube config file to the platform/kube directory and update the paths

    cp ~/.kube/config platform/kube/
    sed -i 's!/Users/<username>!/home/ubuntu!' platform/kube/config

_Note_: The `sed` command above may not work on Windows machines. Replace
the path to certs such that the resultant paths look like
`/home/ubuntu/.minikube/ca.crt`, etc.

### 2. Start Vagrant VM for compiling the code

When you are setting up the VM for the first time,

    vagrant up --provision

For subsequent startups of the VM,

    vagrant up

Your local clone of the istio/manager repository will be mounted in the
Vagrant VM under `/home/ubuntu/go/src/istio.io/manager`.

One time setup in the VM: copy the config file from platform/kube/config
into /home/ubuntu/config

    vagrant ssh
    cp go/src/istio.io/manager/platform/kube/config .kube/config
    sudo chown -R ubuntu:ubuntu .kube

### 3. Build once in the VM

    bazel build //...
    
_Note the three dots_
Create the vendored directories..

    ./bin/init.sh

Login to your docker hub account

    docker login <yourdockeraccount>

Run a end to end test to make sure the VM can talk to minikube

    ./bin/e2e.sh

### 4. Use your favorite IDE on the host

You should now have vendor directories in the manager folder on the
host. You can use your favorite IDE on the host to develop, while using
standard `go` tools. In order to compile project in the vagrant VM, run the
commands described in the the build instructions section below.

### 5. Before you commit

Run the end to end integration tests in the VM

    ./bin/e2e.sh -h docker.io/<yourusername>

Note that this script will push some images to your dockerhub account.
