# Setup and document common environment variables used for building and testing Istio
# User-specific settings can be added to .istiorc in the project workspace or $HOME/.istiorc
# This may include dockerhub settings or other customizations.

# Source the file with: ". envsetup.sh"

export TOP=`pwd`

# Used in the shell scripts.
export ISTIO_SRC=$TOP
export GOPATH=$ISTIO_SRC/go
export ISTIO_GO=$GOPATH/src/istio.io/istio

# hub used to push images.
export HUB=${ISTIO_HUB:-grc.io/istio-testing}
export TAG=${ISTIO_TAG:-$USER}

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
