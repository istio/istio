# To run locally:
# The Makefile will run against a workspace that is expected to have the istio-installer, istio.io/istio and
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
# export MOUNT=1            #local directories mounted in the docker running Kind and tests
# export SKIP_KIND_SETUP=1  #don't create new cluster at each iteration
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

BASE := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
GOPATH = $(shell cd ${BASE}/../../../..; pwd)
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

GO ?= go

TEST_FLAGS ?=  HUB=${HUB} TAG=${TAG}

HELM_VER ?= v2.13.1

# Namespace and environment running the control plane.
# A cluster must support multiple control plane versions.
ISTIO_NS ?= istio-control

# TODO: to convert the tests, we're running telemetry/policy in istio-ns - need new tests or fixes to support
# moving them to separate ns

# Target to run by default inside the builder docker image.
TEST_TARGET ?= run-all-tests

# Required, tests in docker may not execute in /tmp
export TMPDIR = ${GOPATH}/out/tmp

# Setting MOUNT to 1 will mount the current GOPATH into the kind cluster, and run the tests as the
# current user. The default value for kind cluster will be 'local' instead of test.
MOUNT ?= 0

# If SKIP_CLEANUP is set, the cleanup will be skipped even if the test is successful.
SKIP_CLEANUP ?= 0

# SKIP_KIND_SETUP will skip creating a KIND cluster.
SKIP_KIND_SETUP ?= 0

# 60s seems to short for telemetry/prom
WAIT_TIMEOUT ?= 120s

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

# Verify each component can be generated. Create pre-processed yaml files with the defaults.
# TODO: minimize 'ifs' in templates, and generate alternative files for cases we can't remove. The output could be
# used directly with kubectl apply -f https://....
# TODO: Add a local test - to check various things are in the right place (jsonpath or equivalent)
# TODO: run a local etcd/apiserver and verify apiserver accepts the files
run-build: dep
	mkdir -p ${OUT}/release ${TMPDIR}
	cp crds.yaml ${OUT}/release
	bin/iop istio-system istio-system-security ${BASE}/security/citadel -t > ${OUT}/release/citadel.yaml
	bin/iop ${ISTIO_NS} istio-config ${BASE}/istio-control/istio-config -t > ${OUT}/release/istio-config.yaml
	bin/iop ${ISTIO_NS} istio-discovery ${BASE}/istio-control/istio-discovery -t > ${OUT}/release/istio-discovery.yaml
	bin/iop ${ISTIO_NS} istio-autoinject ${BASE}/istio-control/istio-autoinject -t > ${OUT}/release/istio-autoinject.yaml

build:
	docker run -it --rm -v ${GOPATH}:${GOPATH} --entrypoint /bin/bash istionightly/kind:latest \
		-c "cd ${GOPATH}/src/github.com/istio-ecosystem/istio-installer; ls; make run-build"

# Runs the test in docker. Will exec into KIND and run "make $TEST_TARGET" (default: run-all-tests)
docker-run-test:
	docker exec -e KUBECONFIG=/etc/kubernetes/admin.conf  \
		${KIND_CLUSTER}-control-plane \
		bash -c "cd ${GOPATH}/src/github.com/istio-ecosystem/istio-installer; make git.dep ${TEST_TARGET}"

# Run the istio install and tests. Assumes KUBECONFIG is pointing to a valid cluster.
# This should run inside a container and using KIND, to reduce dependency on host or k8s environment.
# It can also be used directly on the host against a real k8s cluster.
run-all-tests: ${TMPDIR} install-crds install-base install-ingress install-telemetry install-policy \
	run-simple run-simple-strict run-bookinfo run-test.integration.kube.presubmit

# Tests running against 'micro' environment - just citadel + pilot + ingress
# TODO: also add 'nano' - pilot + ingress without citadel, some users are using this a-la-carte option
run-micro-tests: install-crds install-base install-ingress run-simple run-simple-strict

# Start a KIND cluster, using current docker environment, and a custom image including helm
# and additional tools to install istio.
prepare:
	mkdir -p ${TMPDIR}
	cat test/kind/kind.yaml | sed s,GOPATH,$(GOPATH), > ${GOPATH}/kind.yaml
	kind create cluster --name ${KIND_CLUSTER} --wait 60s ${KIND_CONFIG} --image istionightly/kind:latest

${TMPDIR}:
	mkdir -p ${TMPDIR}

maybe-prepare:
ifeq ($(SKIP_KIND_SETUP), 0)
	$(MAKE) prepare
endif

clean:
	kind delete cluster --name ${KIND_CLUSTER} 2>&1 || /bin/true

maybe-clean:
ifeq ($(SKIP_CLEANUP), 0)
	$(MAKE) clean
endif

# Install CRDS - only once (in case of repeated runs in dev mode)
/tmp/crds.yaml: crds.yaml
	mkdir -p ${GOPATH}/out/yaml
	cp crds.yaml ${GOPATH}/out/yaml/crds.yaml
	kubectl apply -f crds.yaml
	kubectl wait --for=condition=Established -f crds.yaml
	cp crds.yaml /tmp/crds.yaml

# Will use a temp file to avoid installing crds each time.
# if the crds.yaml changes, the apply will happen again.
install-crds: /tmp/crds.yaml

# Individual step to install or update base istio.
# This setup is optimized for migration from 1.1 and testing - note that autoinject is enabled by default,
# since new integration tests seem to fail to inject
install-base: install-crds
	kubectl create ns ${ISTIO_NS} || /bin/true
	# Autoinject global enabled - we won't be able to install injector
	kubectl label ns ${ISTIO_NS} istio-injection=disabled --overwrite
	bin/iop istio-system istio-system-security ${BASE}/security/citadel ${IOP_OPTS}
	kubectl wait deployments istio-citadel11 -n istio-system --for=condition=available --timeout=${WAIT_TIMEOUT}
	bin/iop ${ISTIO_NS} istio-config ${BASE}/istio-control/istio-config ${IOP_OPTS}
	bin/iop ${ISTIO_NS} istio-discovery ${BASE}/istio-control/istio-discovery ${IOP_OPTS}
	kubectl wait deployments istio-galley istio-pilot -n ${ISTIO_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}
	bin/iop ${ISTIO_NS} istio-autoinject ${BASE}/istio-control/istio-autoinject --set enableNamespacesByDefault=true --set global.istioNamespace=${ISTIO_NS} ${IOP_OPTS}
	kubectl wait deployments istio-sidecar-injector -n ${ISTIO_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}
	# Some tests assumes ingress is in same namespace with pilot.
	# TODO: fix test (or replace), break it down in multiple namespaces for isolation/hermecity
	bin/iop ${ISTIO_NS} istio-ingress ${BASE}/gateways/istio-ingress --set global.istioNamespace=${ISTIO_NS} ${IOP_OPTS}
	kubectl wait deployments ingressgateway -n ${ISTIO_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}


install-ingress:
	bin/iop istio-ingress istio-ingress ${BASE}/gateways/istio-ingress --set global.istioNamespace=${ISTIO_NS} ${IOP_OPTS}
	kubectl wait deployments ingressgateway -n istio-ingress --for=condition=available --timeout=${WAIT_TIMEOUT}

# Telemetry will be installed in istio-control for the tests, until integration tests are changed
# to expect telemetry in separate namespace
install-telemetry:
	#bin/iop istio-telemetry istio-grafana $IBASE/istio-telemetry/grafana/ --set global.istioNamespace=${ISTIO_NS}
	bin/iop ${ISTIO_NS} istio-prometheus ${BASE}/istio-telemetry/prometheus/ --set global.istioNamespace=${ISTIO_NS} ${IOP_OPTS}
	bin/iop ${ISTIO_NS} istio-mixer ${BASE}/istio-telemetry/mixer-telemetry/ --set global.istioNamespace=${ISTIO_NS} ${IOP_OPTS}
	kubectl wait deployments istio-telemetry prometheus -n ${ISTIO_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}

install-policy:
	bin/iop ${ISTIO_NS} istio-policy ${BASE}/istio-policy --set global.istioNamespace=${ISTIO_NS} ${IOP_OPTS}
	kubectl wait deployments istio-policy -n ${ISTIO_NS} --for=condition=available --timeout=${WAIT_TIMEOUT}

# Simple bookinfo install and curl command
run-bookinfo:
	kubectl create ns bookinfo || /bin/true
	echo ${BASE} ${GOPATH}
	# Bookinfo test
	#kubectl label namespace bookinfo istio-env=${ISTIO_NS} --overwrite
	kubectl -n bookinfo apply -f test/k8s/mtls_permissive.yaml
	kubectl -n bookinfo apply -f test/k8s/sidecar-local.yaml
	SKIP_CLEANUP=1 ISTIO_CONTROL=${ISTIO_NS} INGRESS_NS=${ISTIO_NS} SKIP_DELETE=1 SKIP_LABEL=1 bin/test.sh ${GOPATH}/src/istio.io/istio

# Simple fortio install and curl command
#run-fortio:
#	kubectl apply -f

# Will be used by tests
export ISTIOCTL_BIN=/usr/local/bin/istioctl
E2E_ARGS=--skip_setup --use_local_cluster=true --istio_namespace=${ISTIO_NS}

# The simple test from istio/istio integration, in permissive mode
# Will kube-inject and test the ingress and service-to-service
run-simple:
	kubectl create ns simple || /bin/true
	# Global default may be strict or permissive - make it explicit for this ns
	kubectl -n simple apply -f test/k8s/mtls_permissive.yaml
	kubectl -n simple apply -f test/k8s/sidecar-local.yaml
	(cd ${GOPATH}/src/istio.io/istio; make e2e_simple_noauth_run ${TEST_FLAGS} \
		E2E_ARGS="${E2E_ARGS} --namespace=simple")

# Simple test, strict mode
run-simple-strict:
	kubectl create ns simple-strict || /bin/true
	kubectl -n simple-strict apply -f test/k8s/mtls_strict.yaml
	kubectl -n simple apply -f test/k8s/sidecar-local.yaml
	(cd ${GOPATH}/src/istio.io/istio; make e2e_simple_run ${TEST_FLAGS} \
		E2E_ARGS="${E2E_ARGS} --namespace=simple-strict")

run-bookinfo-demo:
	kubectl create ns bookinfo-demo || /bin/true
	kubectl -n simple apply -f test/k8s/mtls_permissive.yaml
	kubectl -n simple apply -f test/k8s/sidecar-local.yaml
	(cd ${GOPATH}/src/istio.io/istio; make e2e_bookinfo_run ${TEST_FLAGS} \
		E2E_ARGS="${E2E_ARGS} --namespace=bookinfo-demo")

run-mixer:
	kubectl create ns mixertest || /bin/true
	kubectl -n mixertest apply -f test/k8s/mtls_permissive.yaml
	kubectl -n mixertest apply -f test/k8s/sidecar-local.yaml
	(cd ${GOPATH}/src/istio.io/istio; make e2e_mixer_run ${TEST_FLAGS} \
		E2E_ARGS="${E2E_ARGS} --namespace=mixertest")

INT_FLAGS ?= -istio.test.hub ${HUB} -istio.test.tag ${TAG} -istio.test.pullpolicy IfNotPresent \
 -p 1 -istio.test.env kube --istio.test.kube.config ${KUBECONFIG} --istio.test.ci --istio.test.nocleanup \
 --istio.test.kube.deploy=false -istio.test.kube.systemNamespace ${ISTIO_NS} -istio.test.kube.minikube

# Integration tests create and delete istio-system
# Need to be fixed to use new installer
# Requires an environment with telemetry installed
run-test.integration.kube:
	export TMPDIR=${GOPATH}/out/tmp
	mkdir -p ${GOPATH}/out/tmp
	#(cd ${GOPATH}/src/istio.io/istio; \
	#	$(GO) test -v ${T} ./tests/integration/... ${INT_FLAGS})
	kubectl -n default apply -f test/k8s/mtls_permissive.yaml
	kubectl -n default apply -f test/k8s/sidecar-local.yaml
	# Test uses autoinject
	#kubectl label namespace default istio-env=${ISTIO_NS} --overwrite
	(cd ${GOPATH}/src/istio.io/istio; TAG=${TAG} make test.integration.kube \
		CI=1 T="-v" K8S_TEST_FLAGS="--istio.test.kube.minikube --istio.test.kube.systemNamespace ${ISTIO_NS} \
		 --istio.test.nocleanup --istio.test.kube.deploy=false ")

run-test.integration.kube.presubmit:
	export TMPDIR=${GOPATH}/out/tmp
	mkdir -p ${GOPATH}/out/tmp
	(cd ${GOPATH}/src/istio.io/istio; TAG=${TAG} make test.integration.kube.presubmit \
		CI=1 T="-v" K8S_TEST_FLAGS="--istio.test.kube.minikube --istio.test.kube.systemNamespace ${ISTIO_NS} \
		 --istio.test.nocleanup --istio.test.kube.deploy=false ")

run-stability:
	 ISTIO_ENV=${ISTIO_NS} bin/iop test stability ${GOPATH}/src/istio.io/tools/perf/stability/allconfig ${IOP_OPTS}

run-mysql:
	 ISTIO_ENV=${ISTIO_NS} bin/iop mysql mysql ${BASE}/test/mysql ${IOP_OPTS}
	 ISTIO_ENV=${ISTIO_NS} bin/iop mysqlplain mysqlplain ${BASE}/test/mysql ${IOP_OPTS} --set mtls=false --set Name=plain

info:
	# Get info about env, for debugging
	set -x
	pwd
	env
	id


# Copy source code from the current machine to the docker.
sync:
ifneq ($(MOUNT), 1)
	docker exec ${KIND_CLUSTER}-control-plane mkdir -p ${GOPATH}/src/github.com/istio-ecosystem/istio-installer \
		${GOPATH}/src/istio.io
	docker cp . ${KIND_CLUSTER}-control-plane:${GOPATH}/src/github.com/istio-ecosystem/istio-installer
endif

# Run an iterative shell in the docker image running the tests and k8s kind
kind-shell:
ifneq ($(MOUNT), 1)
	docker exec -it -e KUBECONFIG=/etc/kubernetes/admin.conf \
		-w ${GOPATH}/src/github.com/istio-ecosystem/istio-installer \
		${KIND_CLUSTER}-control-plane bash
else
	docker exec ${KIND_CLUSTER}-control-plane bash -c "cp /etc/kubernetes/admin.conf /tmp && chown $(shell id -u) /tmp/admin.conf"
	docker exec -it -e KUBECONFIG=/tmp/admin.conf \
		-u "$(shell id -u)" -e USER="${USER}" -e HOME="${GOPATH}" \
		-w ${GOPATH}/src/github.com/istio-ecosystem/istio-installer \
		${KIND_CLUSTER}-control-plane bash
endif

# Run a shell in docker image, as root
kind-shell-root:
	docker exec -it -e KUBECONFIG=/etc/kubernetes/admin.conf \
		-w ${GOPATH}/src/github.com/istio-ecosystem/istio-installer \
		${KIND_CLUSTER}-control-plane bash


# Grab kind logs to $GOPATH/out/logs
kind-logs:
	mkdir -p ${OUT}/logs
	kind export logs --name  ${KIND_CLUSTER} ${OUT}/logs

# Build the Kind+build tools image that will be useed in CI/CD or local testing
# This replaces the istio-builder.
docker.istio-builder: test/docker/Dockerfile ${GOPATH}/bin/istioctl ${GOPATH}/bin/kind ${GOPATH}/bin/helm ${GOPATH}/bin/go-junit-report ${GOPATH}/bin/repo
	mkdir -p ${GOPATH}/out/istio-builder
	curl -Lo - https://github.com/istio/istio/releases/download/${STABLE_TAG}/istio-${STABLE_TAG}-linux.tar.gz | \
    		(cd ${GOPATH}/out/istio-builder;  tar --strip-components=1 -xzf - )
	cp $^ ${GOPATH}/out/istio-builder/
	docker build -t istionightly/kind ${GOPATH}/out/istio-builder

docker.buildkite: test/buildkite/Dockerfile ${GOPATH}/bin/kind ${GOPATH}/bin/helm ${GOPATH}/bin/go-junit-report ${GOPATH}/bin/repo
	mkdir -p ${GOPATH}/out/istio-buildkite
	cp $^ ${GOPATH}/out/istio-buildkite
	docker build -t istionightly/buildkite ${GOPATH}/out/istio-buildkite

# Build or get the dependencies.
dep: ${GOPATH}/bin/kind ${GOPATH}/bin/helm

GITBASE ?= "https://github.com"

${GOPATH}/src/istio.io/istio:
	mkdir -p ${GOPATH}/src/istio.io
	git clone ${GITBASE}/istio/istio.git ${GOPATH}/src/istio.io/istio

${GOPATH}/src/istio.io/tools:
	mkdir -p ${GOPATH}/src/istio.io
	git clone ${GITBASE}/istio/tools.git ${GOPATH}/src/istio.io/tools

#
git.dep: ${GOPATH}/src/istio.io/istio ${GOPATH}/src/istio.io/tools

${GOPATH}/bin/repo:
	curl https://storage.googleapis.com/git-repo-downloads/repo > $@
	chmod +x $@

${GOPATH}/bin/helm:
	mkdir -p ${TMPDIR}
    curl -Lo - https://storage.googleapis.com/kubernetes-helm/helm-${HELM_VER}-linux-amd64.tar.gz | (cd ${GOPATH}/out; tar -zxvf -)
	chmod +x ${GOPATH}/out/linux-amd64/helm
	mv ${GOPATH}/out/linux-amd64/helm ${GOPATH}/bin/helm

# Istio releases: deb and charts on https://storage.googleapis.com/istio-release
#

${GOPATH}/bin/istioctl:
	(cd ${GOPATH}/src/istio.io/istio; make istioctl)
	cp ${GOPATH}/out/linux_amd64/release/istioctl %@

${GOPATH}/bin/kind:
	echo ${GOPATH}
	mkdir -p ${TMPDIR}
	go get -u sigs.k8s.io/kind

${GOPATH}/bin/dep:
	go get -u github.com/golang/dep/cmd/dep

${GOPATH}/bin/go-junit-report:
	go get github.com/jstemmer/go-junit-report


