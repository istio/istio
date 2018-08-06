#!/bin/bash
# Setup and document common environment variables used for building and testing Istio
# User-specific settings can be added to .istiorc in the project workspace or $HOME/.istiorc
# This may include dockerhub settings or other customizations.

# Source the file with: ". envsetup.sh"

export TOP=$(cd ../../..; pwd)

# Used in the shell scripts.
export ISTIO_SRC=$TOP
export GOPATH=$TOP
export ISTIO_GO=$GOPATH/src/istio.io/istio
export PATH=${GOPATH}/bin:$PATH

# hub used to push images.
export HUB=${ISTIO_HUB:-grc.io/istio-testing}
export TAG=${ISTIO_TAG:-$USER}

# Artifacts and temporary files.
export OUT=${GOPATH}/out

if [ -f .istiorc ] ; then
  source .istiorc
fi

if [ -f $HOME/.istiorc ] ; then
  source $HOME/.istiorc
fi


# Runs make at the top of the tree.
function m() {
    (cd $TOP; make $*)
}

# Image used by the circleci, including all tools
export DOCKER_BUILDER=${DOCKER_BUILDER:-istio/ci:go1.9-k8s1.7.4}

# Runs the Istio docker builder image, using the current workspace and user id.
function dbuild() {
  docker run --rm -u $(id -u) -it \
	  --volume /var/run/docker.sock:/var/run/docker.sock \
    -v $TOP:$TOP -w $TOP \
    -e GID=$(id -g) \
    -e USER=$USER \
    -e HOME=$TOP \
    --entrypoint /bin/bash \
    $DOCKER_BUILDER \
    -c "$*"
}

# Lunch a specific environment, by sourcing files specific to the env.
# This allows a developer to work with multiple clusters and environments, without
# overriding or changing the main ~/.kube/config
#
# For each env, create a file under $HOME/.istio/ENV_NAME
#
# Will set TEST_ENV and KUBECONFIG environment variables.
#
# Predefined:
# - minikube: will start a regular minikube in a VM
#
function lunch() {
    local env=$1

    if [[ -f $HOME/.istio/${env} ]]; then
        source $HOME/.istio/${env}
    fi

    if [ "$env" == "minikube" ]; then
        export KUBECONFIG=${OUT}/minikube.conf
        if minikube status; then
          minikube start
        fi
    fi

    export TEST_ENV=$env
}

function stop() {
    if [ "$env" == "minikube" ]; then
        minikube stop
    fi
}
