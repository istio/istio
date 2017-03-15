
# Vagrant build environment

_Note:_ This section applies to Mac and Windows users only. You can develop natively on Linux.

## Pre-requisites ##

- Setup Go 1.8+ on your host machine
- Clone this repository
- Install [Virtualbox](https://github.com/kubernetes/minikube/releases)
- Install [Minikube](https://github.com/kubernetes/minikube/releases)
- Install [Vagrant](https://www.vagrantup.com/downloads.html)
- Install [kubectl](https://kubernetes.io/docs/user-guide/prereqs/)

## 1. Start Minikube

Istio Manager needs a recent kubernetes version (see [testing doc](testing.md)).

    minikube config set kubernetes-version v1.x.y
    minikube start

Copy the kube config file to the platform/kube directory and update the paths

    cp ~/.kube/config platform/kube/
    sed -i 's!/Users/<username>!/home/ubuntu!' platform/kube/config

_Note_: The `sed` command above may not work on Windows machines. Replace
the path to certs such that the resultant paths look like
`/home/ubuntu/.minikube/ca.crt`, etc.

## 2. Start Vagrant VM for compiling the code

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

## 3. Build once in the VM

    bazel build //...

_Note the three dots_
Create the vendored directories..

    ./bin/init.sh

Login to your docker hub account

    docker login <yourdockeraccount>

Run a end to end test to make sure the VM can talk to minikube

    ./bin/e2e.sh

## 4. Use your favorite IDE on the host

You should now have vendor directories in the manager folder on the
host. You can use your favorite IDE on the host to develop, while using
standard `go` tools. In order to compile project in the vagrant VM, run the
commands described in the the build instructions section below.

## 5. Before you commit

Run the end to end integration tests in the VM

    ./bin/e2e.sh -hub docker.io/<yourusername>

Note that this script will push some images to your dockerhub account.
