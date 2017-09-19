#!/bin/bash

# Helpers for checking out and building latest version of Istio (with minimal/no use of
# manual SHAs). This is an in-progress proposal/PoC for a consistent build.

# Environment:
# ISTIO_BASE: base directory where istio will be checked out or built. Defaults to GOPATH.
#  The script will populate or update the version.
# HUB: hub to use to upload the docker images.
# TAG: tag to use for the docker images. Defaults to user ID.

ISTIO_BASE=${ISTIO_BASE:-${GOPATH:-${HOME}/go}}
ISTIO_IO=${ISTIO_IO:-${ISTIO_BASE}/src/istio.io}

HUB=${HUB:-gcr.io/istio-testing}
BRANCH=${ISTIO_BRANCH:-master}
REMOTE=${ISTIO_REMOTE:-origin}


# Build all components using bazel.
function istio_build() {

  # Note: components may still use old SHA - but the test will build the binaries from master
  # from each component, to make sure we don't test old code.
  (cd $ISTIO_IO/pilot; bazel build cmd/... tools/deb/...)

  (cd $ISTIO_IO/mixer; bazel build ...)

  (cd $ISTIO_IO/proxy; bazel build tools/deb/... src/envoy/mixer:envoy)

  (cd $ISTIO_IO/auth; bazel build ...)
}

# Pull from master, equivalent with "repo sync"
function istio_sync() {
  # TODO: use "repo sync" instead
  # TODO: sync on green build ( if repo is used )
  mkdir -p $ISTIO_IO

  for sub in pilot istio mixer auth proxy; do
    if [[ -d $ISTIO_IO/$sub ]]; then
      echo "Syncing $sub"
      (cd $ISTIO_IO/$sub; git pull origin master)
    else
      (cd $ISTIO_IO; git clone https://github.com/istio/$sub; )
    fi
  done

}

# Show the branch and status of each istio repo.
# Similar with "repo status"
function istio_status() {
  cd $ISTIO_IO

  for sub in pilot istio mixer auth proxy; do
     echo -e "\n\n$sub\n"
     (cd $ISTIO_IO/$sub; git branch; git status)
  done
}

# Checkout all repos at a specific tag
# Example: istio_checkout 0.2.2
function istio_checkout {
  local TAG=$1
  cd $ISTIO_IO

  for sub in pilot istio mixer auth proxy; do
     echo -e "\n\n$sub\n"
     (cd $ISTIO_IO/$sub; git checkout $TAG)
  done
}

# Create a tag i
function istio_tag {
  local TAG=$1
  cd $ISTIO_IO

  for sub in pilot istio mixer auth proxy; do
     echo -e "\n\n$sub\n"
     (cd $ISTIO_IO/$sub; git tag $TAG)
  done
}

# Build docker images for istio from current branch, using same tag for all.
#
function istio_build_docker() {
  local TAG=${1:-${TAG:-$(whoami)}}
  # Will create a local docker image gcr.io/istio-testing/envoy-debug:USERNAME

  (cd $ISTIO_IO/proxy; TAG=$TAG ./script/release-docker debug)

  gcloud docker -- push $HUB/envoy-debug:$TAG

  # TODO: proxy will still use a hardcoded version, from the dockerfile.
  (cd $ISTIO_IO/pilot; ./bin/push-docker -tag $TAG)

  (cd $ISTIO_IO/auth; ./bin/push-docker.sh -t $TAG -h $HUB)

  (cd $ISTIO_IO/mixer; ./bin/publish-docker-images.sh -h $HUB -t $TAG)

}

# Run the updateVersion script with the expected tag parameters.
function istio_update_version() {
  local TAG=${1:-$(whoami)}

  (cd $ISTIO_IO/istio; ./install/updateVersion.sh -p $HUB,$TAG -x $HUB,$TAG -c $HUB,$TAG)
}

# Run the tests with the images built by istio_build_docker.
function istio_test() {
  local TAG=${TAG:-$(whoami)}

  # Using head istioctl (no download)
  (cd $ISTIO_IO/istio; ./tests/e2e.sh --auth_enable --rbac_path=install/kubernetes/istio-rbac-beta.yaml --skip_cleanup --namespace e2e --mixer_hub $HUB --mixer_tag $TAG \
    --istioctl $ISTIO_IO/pilot/bazel-bin/cmd/istioctl/istioctl --pilot_hub $HUB --pilot_tag $TAG --ca_hub $HUB --ca_tag $TAG --project_id $(whoami)-istio )
}

# Rerun a test.
function istio_retest() {
  locat TESTS=$1
  local TAG=${TAG:-$(whoami)}

  (cd $ISTIO_IO/istio; ./tests/e2e.sh --auth_enable --rbac_path=install/kubernetes/istio-rbac-beta.yaml --skip_cleanup --skip_setup -test.run=$TESTS --namespace e2e --mixer_hub $HUB --mixer_tag $TAG \
    --istioctl $ISTIO_IO/pilot/bazel-bin/cmd/istioctl/istioctl --pilot_hub $HUB --pilot_tag $TAG --ca_hub $HUB --ca_tag $TAG --project_id $(whoami)-istio )
}

# Build a local docker image with the VM components installed.
function istio_build_vm_docker() {

    # Image has a fixed name, you can tag and override it to upload to a repo.
    local DOCKER_IMAGE=${DOCKER_IMAGE:-istio-vm-test}

    # Staging dir
    local OUT=$ISTIO_IO/istio/bazel-bin/vm

    mkdir -p $OUT
    cp -f $ISTIO_IO/proxy/bazel-bin/tools/deb/istio-proxy-envoy.deb $OUT
    cp -f $ISTIO_IO/auth/bazel-bin/tools/deb/istio-auth-node-agent.deb $OUT
    cp -f $ISTIO_IO/pilot/bazel-bin/tools/deb/istio-agent.deb $OUT
    cp -f $ISTIO_IO/istio/tests/local/* $OUT

    docker build -f $OUT/Dockerfile -t "${DOCKER_IMAGE}" $OUT
}
