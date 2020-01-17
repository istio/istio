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
# export ONE_NAMESPACE=1    # deploy all Istio components in one namespace
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
HUB ?= gcr.io/istio-testing
TAG ?= latest
export HUB
export TAG
EXTRA ?= --set global.hub=${HUB} --set global.tag=${TAG}
OUT ?= ${TOP}/out

BUILD_IMAGE ?= istionightly/ci:2019-08-30

GO ?= go

TEST_FLAGS ?=  HUB=${HUB} TAG=${TAG}

HELM_VER ?= v2.13.1

# Will be used by tests. In CI 'machine' or some other cases needs to be built locally, we can't
# customize base image
export ISTIOCTL_BIN ?= /usr/local/bin/istioctl

# This enables injection of sidecar in all namespaces with the default sidecar.
# if it is false, use the specified sidecar.
ENABLE_NAMESPACES_BY_DEFAULT ?= true
CUSTOM_SIDECAR_INJECTOR_NAMESPACE ?=

# Namespace and environment running the control plane.
# A cluster must support multiple control plane versions.
ISTIO_SYSTEM_NS ?= istio-system
ISTIO_TESTING_NS ?= istio-testing
# Default is ONE_NAMESPACE for 1.3. Set it to 0 to test multiple control planes.
ONE_NAMESPACE ?= 1

ifeq ($(ONE_NAMESPACE), 1)
ISTIO_CONTROL_NS ?= ${ISTIO_SYSTEM_NS}
ISTIO_TELEMETRY_NS ?= ${ISTIO_SYSTEM_NS}
ISTIO_POLICY_NS ?= ${ISTIO_SYSTEM_NS}
ISTIO_INGRESS_NS ?= ${ISTIO_SYSTEM_NS}
ISTIO_EGRESS_NS ?= ${ISTIO_SYSTEM_NS}
else
ISTIO_CONTROL_NS ?= istio-control
ISTIO_TELEMETRY_NS ?= istio-telemetry
ISTIO_POLICY_NS ?= istio-policy
ISTIO_INGRESS_NS ?= istio-ingress
ISTIO_EGRESS_NS ?= istio-egress
	ifeq ($(ENABLE_NAMESPACES_BY_DEFAULT),false)
		CUSTOM_SIDECAR_INJECTOR_NAMESPACE = istio-control
	endif
endif

# Namespace for running components with admin rights, e.g. kiali
ISTIO_ADMIN_NS ?= istio-admin

# TODO: to convert the tests, we're running telemetry/policy in istio-ns - need new tests or fixes to support
# moving them to separate ns

# Target to run by default inside the builder docker image.
TEST_TARGET ?= run-all-tests

# Required, tests in docker may not execute in /tmp
export TMPDIR=${TOP}/out/tmp

# Setting MOUNT to 1 will mount the current TOP into the kind cluster, and run the tests as the
# current user. The default value for kind cluster will be 'local' instead of test.
# If MOUNT is 0, 'make sync' will copy files from the local workspace/git into the docker container running the tests
# and kind.
MOUNT ?= 0

# If SKIP_CLEANUP is set, the cleanup will be skipped even if the test is successful.
SKIP_CLEANUP ?= 0

# SKIP_KIND_SETUP will skip creating a KIND cluster.
SKIP_KIND_SETUP ?= 0

# 60s seems to short for telemetry/prom
WAIT_TIMEOUT ?= 240s

IOP_OPTS="-f test/kind/user-values.yaml"

KIND_CONFIG ?= --config ${TOP}/kind.yaml
ifeq ($(MOUNT), 1)
	KIND_CLUSTER ?= local
else
	# Customize this if you want to run tests in different kind clusters in parallel
	# For example you can try a config in 'test' cluster, and run other tests in 'test2' while you
	# debug or tweak 'test'.
	# Remember that 'make test' will clean the previous cluster at startup - but leave it running after in case of errors,
	# so failures can be debugged.
	KIND_CLUSTER ?= test
	LOCALVOL=""
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
#
# Make test will cleanup the previous run at startup
test: pretest docker-run-test maybe-clean

# Retest is like test, but without creating the cluster and cleanup. Use it to repeat tests in case of failure.
retest: sync docker-run-test

pretest: info dep maybe-clean maybe-prepare sync

# Build all templates inside the hermetic docker image. Tools are installed.
build:
	docker run -it --rm -v ${TOP}:${TOP} --entrypoint /bin/bash -e GOPATH=${GOPATH} \
		$(BUILD_IMAGE) \
		-c "cd ${TOP}/src/istio.io/installer; ls; make run-build"

# Run a command in the docker image running kind. Command passed as "TARGET" env.
kind-run:
	docker exec -e KUBECONFIG=/etc/kubernetes/admin.conf -e ONE_NAMESPACE=$(ONE_NAMESPACE) \
		${KIND_CLUSTER}-control-plane \
		bash -c "cd ${TOP}/src/istio.io/installer; make ${TARGET}"


# Runs the test in docker. Will exec into KIND and run "make $TEST_TARGET" (default: run-all-tests)
docker-run-test:
	docker exec -e KUBECONFIG=/etc/kubernetes/admin.conf -e ONE_NAMESPACE=$(ONE_NAMESPACE) \
		-e GO111MODULE=on -e GOPROXY=${GOPROXY} ${KIND_CLUSTER}-control-plane \
		bash -c "cd ${TOP}/src/istio.io/installer; make git.dep ${TEST_TARGET}"

# Start a KIND cluster, using current docker environment, and a custom image including helm
# and additional tools to install istio.
prepare:
ifeq ($(MOUNT), 1)
	mkdir -p ${TMPDIR}
	# This will mount the directory inside the kind cluster, no need to copy
	cat test/kind/kind.yaml | sed s,TOP,$(TOP), > ${TOP}/kind.yaml
	# Kind version and node image need to be in sync - that's not ideal, working on a fix.
	kind create cluster --loglevel debug --name ${KIND_CLUSTER} --wait 60s ${KIND_CONFIG} --image $(BUILD_IMAGE)
else
	# Not this will create a kind cluster inside a docker container - the kube config will be in the container.
	docker run --privileged \
		-v /var/run/docker.sock:/var/run/docker.sock  \
		-e GOPATH=${TOP} \
		-v ${BASE}:/workspace \
		-it --entrypoint /bin/bash --rm \
		$(BUILD_IMAGE) -c \
		"/usr/local/bin/kind create cluster --loglevel debug --name ${KIND_CLUSTER} --wait 60s --config /workspace/test/kind/kind-docker.yaml  --image $(BUILD_IMAGE)"
endif
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
		-e GOPATH=${TOP} \
		-it --entrypoint /bin/bash --rm \
		$(BUILD_IMAGE) -c \
		"/usr/local/bin/kind delete cluster --name ${KIND_CLUSTER}" 2>&1 || true
	#kind delete cluster --name ${KIND_CLUSTER} 2>&1 || true

maybe-clean:
ifeq ($(SKIP_CLEANUP), 0)
	$(MAKE) clean
endif

info:
	# Get info about env, for debugging
	set -x
	pwd
	env
	id

gen-check: check-clean-repo
	git diff

# Copy source code from the current machine to the docker.
sync:
ifneq ($(MOUNT), 1)
	docker exec ${KIND_CLUSTER}-control-plane mkdir -p ${TOP}/src/istio.io/installer \
		${TOP}/src/istio.io
	docker cp . ${KIND_CLUSTER}-control-plane:${TOP}/src/istio.io/installer
endif

# Run an iterative shell in the docker image running the tests and k8s kind
kind-shell:
ifneq ($(MOUNT), 1)
	docker exec -it -e KUBECONFIG=/etc/kubernetes/admin.conf \
		-w ${TOP}/src/istio.io/installer \
		${KIND_CLUSTER}-control-plane bash
else
	docker exec ${KIND_CLUSTER}-control-plane bash -c "cp /etc/kubernetes/admin.conf /tmp && chown $(shell id -u) /tmp/admin.conf"
	docker exec -it -e KUBECONFIG=/tmp/admin.conf \
		-u "$(shell id -u)" -e USER="${USER}" -e HOME="${TOP}" \
		-w ${TOP}/src/istio.io/installer \
		${KIND_CLUSTER}-control-plane bash
endif

# Run a shell in docker image, as root
kind-shell-root:
	docker exec -it -e KUBECONFIG=/etc/kubernetes/admin.conf \
		-w ${TOP}/src/istio.io/installer \
		${KIND_CLUSTER}-control-plane bash


# Grab kind logs to $TOP/out/logs
kind-logs:
	mkdir -p ${OUT}/logs
	kind export logs  --loglevel debug --name ${KIND_CLUSTER} ${OUT}/logs || true

# Build the Kind+build tools image that will be useed in CI/CD or local testing
# This replaces the istio-builder.
docker.istio-builder: test/docker/Dockerfile
	mkdir -p ${TOP}/out/istio-builder
	curl -Lo - https://github.com/istio/istio/releases/download/${STABLE_TAG}/istio-${STABLE_TAG}-linux.tar.gz | \
    		(cd ${TOP}/out/istio-builder;  tar --strip-components=1 -xzf - )
	cp $^ ${TOP}/out/istio-builder/
	docker build -t $(BUILD_IMAGE) ${TOP}/out/istio-builder

push.docker.istio-builder:
	docker push $(BUILD_IMAGE)

docker.buildkite: test/buildkite/Dockerfile
	mkdir -p ${TOP}/out/istio-buildkite
	cp $^ ${TOP}/out/istio-buildkite
	docker build -t istionightly/buildkite ${TOP}/out/istio-buildkite

# Build or get the dependencies.
dep:

GITBASE ?= "https://github.com"

${TOP}/src/istio.io/istio:
	mkdir -p ${TOP}/src/istio.io
	git clone ${GITBASE}/istio/istio.git ${TOP}/src/istio.io/istio

${TOP}/src/istio.io/tools:
	mkdir -p ${TOP}/src/istio.io
	git clone ${GITBASE}/istio/tools.git ${TOP}/src/istio.io/tools

gitdep:
	mkdir -p ${TOP}/tmp
	mkdir -p ${TOP}/src/istio.io/
	git clone https://github.com/istio/istio.git ${TOP}/src/istio.io/istio
	git clone https://github.com/istio/tools.git ${TOP}/src/istio.io/tools

#
git.dep: ${TOP}/src/istio.io/istio ${TOP}/src/istio.io/tools

${TOP}/src/sigs.k8s.io/kind:
	git clone https://github.com/sigs.k8s.io/kind ${TOP}/src/sigs.k8s.io/kind

# Istio releases: deb and charts on https://storage.googleapis.com/istio-release
#
${GOBIN}/kind: ${TOP}/src/sigs.k8s.io/kind
	echo ${TOP}
	mkdir -p ${TMPDIR}
	GO111MODULE="on" go get -u sigs.k8s.io/kind@master

${GOBIN}/dep:
	go get -u github.com/golang/dep/cmd/dep

lint: lint_modern

lint-helm-global:
	${FINDFILES} -name 'Chart.yaml' -print0 | ${XARGS} -L 1 dirname | xargs -r helm lint --strict -f global.yaml

lint_modern: lint-go lint-python lint-copyright-banner lint-markdown lint-protos lint-helm-global

.PHONY: istioctl
istioctl: ${GOBIN}/istioctl
${GOBIN}/istioctl:
	mkdir -p ${TMPDIR}
	cd ${TOP}/src/istio.io/istio; go install ./istioctl/cmd/istioctl

include test/install.mk
include test/tests.mk
include test/noauth.mk
include test/demo.mk
include test/canary/canary.mk
include common/Makefile.common.mk
