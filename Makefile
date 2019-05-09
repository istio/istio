# To run locally:
# The Makefile will run against a workspace that is expected to have the installer, istio.io/istio and
# istio.io/tools repositories checked out. It will copy (TODO: or mount) the sources in a KIND docker container that
# has all the tools needed to build and test.
#
# "make test" is the main command - and should be used in all CI/CD systems and for clean local testing.
#
# For local development, after 'make test' you can run individual steps or debug - the KIND cluster will be wiped
# the next time you run 'make test', but will be kept around for debugging and repeat tests.
#
# Example local workflow for development/testing:
#
# export KIND_CLUSTER=local # or other clusters if working on multiple PRs
# export MOUNT=1            # local directories mounted in the docker running Kind and tests
# export SKIP_KIND_SETUP=1  # don't create new cluster at each iteration
# export SKIP_CLEANUP=1     # leave cluster and tests in place, for debugging
#
# - prepare cluster for development:
#     make prepare
# - install or reinstal istio components needed for test (repeat after rebuilding istio):
#   This step currently needs uploaded images or 'kind load' to copy them into kind.
#   (TODO: build istio in the kind container, so we don't have to upload)
#
#     make TEST_TARGET="install-base"
#
# - Run individual tests using:
#     make TEST_TARGET="run-simple" # or other run- targets.
#  alternatively, run "make kind-shell" and run "make run-simple" or "cd ...istio.io/istio" and go test ...
#  The tests MUST run inside the Kind container - so they have access to 'pods', ingress, etc.
#
#  On the host, you can run "ps" and dlv attach for remote debugging. The tests run inside the KIND docker container,
#  so they have access to the pods and apiserver, as well as the ingress and node.
#
# - make clean - when done
#
# - make kind-shell  - run a shell in the kind container.
# - make kind-logs - get logs from all components
#
# Fully hermetic test:
# - make test - will clean 'test' KIND cluster, create a new one, run the tests.
# In case of failure:
# - make test SKIP_KIND_SETUP=1 SKIP_CLEANUP=1 TEST_TARGET=(target that failed)
#
SHELL := /bin/bash

BASE := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
GOPATH ?= $(shell cd ${BASE}/../../..; pwd)
TOP ?= $(GOPATH)
export GOPATH
# TAG of the last stable release of istio. Used for upgrade testing and to verify istioctl.
# TODO: make sure a '1.1' tag is applied to latest minor release, or have a manifest we can download
STABLE_TAG = 1.1.0
HUB ?= istionightly
TAG ?= nightly-master
export HUB
export TAG
EXTRA ?= --set global.hub=${HUB} --set global.tag=${TAG}
OUT ?= ${GOPATH}/out

BUILD_IMAGE ?= istionightly/kind:v1.14.1-1

GO ?= go

TEST_FLAGS ?=  HUB=${HUB} TAG=${TAG}

HELM_VER ?= v2.13.1

# Will be used by tests. In CI 'machine' or some other cases needs to be built locally, we can't
# customize base image
export ISTIOCTL_BIN ?= /usr/local/bin/istioctl

# Namespace and environment running the control plane.
# A cluster must support multiple control plane versions.
ISTIO_NS ?= istio-control

# TODO: to convert the tests, we're running telemetry/policy in istio-ns - need new tests or fixes to support
# moving them to separate ns

# Target to run by default inside the builder docker image.
TEST_TARGET ?= run-all-tests

# Required, tests in docker may not execute in /tmp
export TMPDIR=${GOPATH}/out/tmp

# Setting MOUNT to 1 will mount the current GOPATH into the kind cluster, and run the tests as the
# current user. The default value for kind cluster will be 'local' instead of test.
MOUNT ?= 0

# If SKIP_CLEANUP is set, the cleanup will be skipped even if the test is successful.
SKIP_CLEANUP ?= 0

# SKIP_KIND_SETUP will skip creating a KIND cluster.
SKIP_KIND_SETUP ?= 0

# 60s seems to short for telemetry/prom
WAIT_TIMEOUT ?= 240s

IOP_OPTS="-f test/kind/user-values.yaml"

ifeq ($(MOUNT), 1)
	KIND_CONFIG ?= --config ${GOPATH}/kind.yaml
	KIND_CLUSTER ?= local
else
	# Customize this if you want to run tests in different kind clusters in parallel
	# For example you can try a config in 'test' cluster, and run other tests in 'test2' while you
	# debug or tweak 'test'.
	# Remember that 'make test' will clean the previous cluster at startup - but leave it running after in case of errors,
	# so failures can be debugged.
	KIND_CLUSTER ?= test

	KIND_CONFIG =
endif

KUBECONFIG ?= ~/.kube/config

# Run the tests, creating a clean test KIND cluster
# Test can also run in a CI/CD system capable of Docker priv execution.
# All tests are run in a container - not on local machine.
# Assumes the installer is checked out, and is the current working directory.
#
# Will remove the 'test' kind cluster and create a new one. After running this target you can debug
# and re-run individual tests - next time the target is run it will cleanup.
#
# For example:
# 1. make test
# 2. Errors reported
# 3. make sync ( to copy modified files - unless MOUNT=1 was used)
# 4. make docker-run-test - run the tests in the same KIND environment
test: info dep maybe-clean maybe-prepare sync docker-run-test maybe-clean


# Build all templates inside the hermetic docker image. Tools are installed.
build:
	docker run -it --rm -v ${GOPATH}:${GOPATH} --entrypoint /bin/bash $(BUILD_IMAGE) \
		-e GOPATH=${GOPATH} \
		-c "cd ${GOPATH}/src/istio.io/installer; ls; make run-build"

# Run a command in the docker image running kind. Command passed as "TARGET" env.
kind-run:
	docker exec -e KUBECONFIG=/etc/kubernetes/admin.conf  \
		${KIND_CLUSTER}-control-plane \
		bash -c "cd ${GOPATH}/src/istio.io/installer; make ${TARGET}"

# Runs the test in docker. Will exec into KIND and run "make $TEST_TARGET" (default: run-all-tests)
docker-run-test:
	docker exec -e KUBECONFIG=/etc/kubernetes/admin.conf  \
		${KIND_CLUSTER}-control-plane \
		bash -c "cd ${GOPATH}/src/istio.io/installer; make git.dep ${TEST_TARGET}"

# Start a KIND cluster, using current docker environment, and a custom image including helm
# and additional tools to install istio.
prepare:
	mkdir -p ${TMPDIR}
	cat test/kind/kind.yaml | sed s,GOPATH,$(GOPATH), > ${GOPATH}/kind.yaml
	# Kind version and node image need to be in sync - that's not ideal, working on a fix.
	docker run --privileged \
		-v /var/run/docker.sock:/var/run/docker.sock  \
		-e GOPATH=${GOPATH} \
		-it --entrypoint /bin/bash --rm \
		istionightly/kind:v1.14.1-1 -c \
		"/usr/local/bin/kind create cluster --loglevel debug --name ${KIND_CLUSTER} --wait 60s ${KIND_CONFIG} --image $(BUILD_IMAGE)"

	#kind create cluster --loglevel debug --name ${KIND_CLUSTER} --wait 60s ${KIND_CONFIG} --image $(BUILD_IMAGE)

${TMPDIR}:
	mkdir -p ${TMPDIR}

maybe-prepare:
ifeq ($(SKIP_KIND_SETUP), 0)
	$(MAKE) prepare
endif

clean:
	docker run --privileged \
		-v /var/run/docker.sock:/var/run/docker.sock  \
		-e GOPATH=${GOPATH} \
		-it --entrypoint /bin/bash --rm \
		istionightly/kind:v1.14.1-1 -c \
		"/usr/local/bin/kind delete cluster --name ${KIND_CLUSTER}" 2>&1 || true
	#kind delete cluster --name ${KIND_CLUSTER} 2>&1 || true

maybe-clean:
ifeq ($(SKIP_CLEANUP), 0)
	$(MAKE) clean
endif

demo-install:
	bin/install.sh

# Generate junit reports, upload to testgrid, fail if conditions are met.
# Failure is based on test status - may exclude some tests.
report:
	bin/report.sh

info:
	# Get info about env, for debugging
	set -x
	pwd
	env
	id


# Copy source code from the current machine to the docker.
sync:
ifneq ($(MOUNT), 1)
	docker exec ${KIND_CLUSTER}-control-plane mkdir -p ${GOPATH}/src/istio.io/installer \
		${GOPATH}/src/istio.io
	docker cp . ${KIND_CLUSTER}-control-plane:${GOPATH}/src/istio.io/installer
endif

# Run an iterative shell in the docker image running the tests and k8s kind
kind-shell:
ifneq ($(MOUNT), 1)
	docker exec -it -e KUBECONFIG=/etc/kubernetes/admin.conf \
		-w ${GOPATH}/src/istio.io/installer \
		${KIND_CLUSTER}-control-plane bash
else
	docker exec ${KIND_CLUSTER}-control-plane bash -c "cp /etc/kubernetes/admin.conf /tmp && chown $(shell id -u) /tmp/admin.conf"
	docker exec -it -e KUBECONFIG=/tmp/admin.conf \
		-u "$(shell id -u)" -e USER="${USER}" -e HOME="${GOPATH}" \
		-w ${GOPATH}/src/istio.io/installer \
		${KIND_CLUSTER}-control-plane bash
endif

# Run a shell in docker image, as root
kind-shell-root:
	docker exec -it -e KUBECONFIG=/etc/kubernetes/admin.conf \
		-w ${GOPATH}/src/istio.io/installer \
		${KIND_CLUSTER}-control-plane bash


# Grab kind logs to $GOPATH/out/logs
kind-logs:
	mkdir -p ${OUT}/logs
	kind export logs  --loglevel debug --name ${KIND_CLUSTER} ${OUT}/logs || true

# Build the Kind+build tools image that will be useed in CI/CD or local testing
# This replaces the istio-builder.
docker.istio-builder: test/docker/Dockerfile
	mkdir -p ${GOPATH}/out/istio-builder
	curl -Lo - https://github.com/istio/istio/releases/download/${STABLE_TAG}/istio-${STABLE_TAG}-linux.tar.gz | \
    		(cd ${GOPATH}/out/istio-builder;  tar --strip-components=1 -xzf - )
	cp $^ ${GOPATH}/out/istio-builder/
	docker build -t $(BUILD_IMAGE) ${GOPATH}/out/istio-builder

push.docker.istio-builder:
	docker push $(BUILD_IMAGE)

docker.buildkite: test/buildkite/Dockerfile
	mkdir -p ${GOPATH}/out/istio-buildkite
	cp $^ ${GOPATH}/out/istio-buildkite
	docker build -t istionightly/buildkite ${GOPATH}/out/istio-buildkite

# Build or get the dependencies.
dep:

GITBASE ?= "https://github.com"

${GOPATH}/src/istio.io/istio:
	mkdir -p ${GOPATH}/src/istio.io
	git clone ${GITBASE}/istio/istio.git ${GOPATH}/src/istio.io/istio

${GOPATH}/src/istio.io/tools:
	mkdir -p ${GOPATH}/src/istio.io
	git clone ${GITBASE}/istio/tools.git ${GOPATH}/src/istio.io/tools

gitdep:
	mkdir -p ${GOPATH}/tmp
	mkdir -p ${GOPATH}/src/istio.io/
	git clone https://github.com/istio/istio.git ${GOPATH}/src/istio.io/istio
	git clone https://github.com/istio/tools.git ${GOPATH}/src/istio.io/tools

#
git.dep: ${GOPATH}/src/istio.io/istio ${GOPATH}/src/istio.io/tools

# Istio releases: deb and charts on https://storage.googleapis.com/istio-release
#
${GOPATH}/bin/kind:
	echo ${GOPATH}
	mkdir -p ${TMPDIR}
	git clone https://github.com/kubernetes-sigs/kind ${GOPATH}/src/sigs.k8s.io/kind && cd ${GOPATH}/src/sigs.k8s.io/kind && git checkout v0.2.1 && go install .

${GOPATH}/bin/dep:
	go get -u github.com/golang/dep/cmd/dep

lint:
	$(MAKE) kind-run TARGET="run-lint"

include test/install.mk
include test/tests.mk
include test/noauth.mk
include test/demo.mk
