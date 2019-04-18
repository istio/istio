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
#     make TEST_TARGET="run-simple-istio-system"
#
#  On the host, you can run "ps" and dlv attach for remote debugging. The tests run inside the KIND docker container,
#  so they have access to the pods and apiserver, as well as the ingress and node.
#
# - make clean - when done
#
# - make kind-shell  - run a shell in the kind container.
# - make kind-logs - get logs from all components

BASE := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
GOPATH = $(shell cd ${BASE}/../../../..; pwd)
TOP ?= $(GOPATH)

EXTRA ?= --set global.hub=${HUB} --set global.tag=${TAG}
OUT ?= ${GOPATH}/out

HELM_VER ?= v2.13.1

# Target to run by default inside the builder docker image.
TEST_TARGET ?= run-all-tests

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
	mkdir ${OUT}/release
	cp crds.yaml ${OUT}/release
	bin/iop istio-system istio-system-security ${BASE}/security/citadel -t > ${OUT}/release/citadel.yaml
	bin/iop istio-control istio-config ${BASE}/istio-control/istio-config -t > ${OUT}/release/istio-config.yaml
	bin/iop istio-control istio-discovery ${BASE}/istio-control/istio-discovery -t > ${OUT}/release/istio-discovery.yaml
	bin/iop istio-control istio-autoinject ${BASE}/istio-control/istio-autoinject -t > ${OUT}/release/istio-autoinject.yaml

build:
	docker run -it --rm -v ${GOPATH}:${GOPATH} --entrypoint /bin/bash istionightly/kind:latest \
		-c "cd ${GOPATH}/src/github.com/istio-ecosystem/istio-installer; ls; make run-build"

# Runs the test in docker
docker-run-test:
	docker exec -e KUBECONFIG=/etc/kubernetes/admin.conf  \
		${KIND_CLUSTER}-control-plane \
		bash -c "cd ${GOPATH}/src/github.com/istio-ecosystem/istio-installer; make git.dep ${TEST_TARGET}"

# Run the istio install and tests. Assumes KUBECONFIG is pointing to a valid cluster.
# This should run inside a container and using KIND, to reduce dependency on host or k8s environment.
# It can also be used directly on the host against a real k8s cluster.
run-all-tests: install-crds install-base install-ingress run-bookinfo

run-sanity-tests: install-crds install-base install-ingress run-bookinfo

# Start a KIND cluster, using current docker environment, and a custom image including helm
# and additional tools to install istio.
prepare:
	cat test/kind/kind.yaml | sed s,GOPATH,$(GOPATH), > ${GOPATH}/kind.yaml
	kind create cluster --name ${KIND_CLUSTER} --wait 60s ${KIND_CONFIG} --image istionightly/kind:latest

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

install-crds: /tmp/crds.yaml

# Individual step to install or update base istio.
install-base: install-crds
	bin/iop istio-system istio-system-security ${BASE}/security/citadel ${IOP_OPTS}
	kubectl wait deployments istio-citadel11 -n istio-system --for=condition=available --timeout=${WAIT_TIMEOUT}
	bin/iop istio-control istio-config ${BASE}/istio-control/istio-config ${IOP_OPTS}
	bin/iop istio-control istio-discovery ${BASE}/istio-control/istio-discovery ${IOP_OPTS}
	bin/iop istio-control istio-autoinject ${BASE}/istio-control/istio-autoinject --set global.istioNamespace=istio-control ${IOP_OPTS}
	kubectl wait deployments istio-galley istio-pilot istio-sidecar-injector -n istio-control --for=condition=available --timeout=${WAIT_TIMEOUT}


install-ingress:
	bin/iop istio-ingress istio-ingress ${BASE}/gateways/istio-ingress --set global.istioNamespace=istio-control ${IOP_OPTS}
	kubectl wait deployments ingressgateway -n istio-ingress --for=condition=available --timeout=${WAIT_TIMEOUT}

install-telemetry:
	#bin/iop istio-telemetry istio-grafana $IBASE/istio-telemetry/grafana/ --set global.istioNamespace=istio-control
	bin/iop istio-telemetry istio-prometheus ${BASE}/istio-telemetry/prometheus/ --set global.istioNamespace=istio-control ${IOP_OPTS}
	bin/iop istio-telemetry istio-mixer ${BASE}/istio-telemetry/mixer-telemetry/ --set global.istioNamespace=istio-control ${IOP_OPTS}
	kubectl wait deployments istio-telemetry prometheus -n istio-telemetry --for=condition=available --timeout=${WAIT_TIMEOUT}

# Simple bookinfo install and curl command
run-bookinfo:
	echo ${BASE} ${GOPATH}
	# Bookinfo test
	SKIP_CLEANUP=1 bin/test.sh ${GOPATH}/src/istio.io/istio

# Simple fortio install and curl command
#run-fortio:
#	kubectl apply -f

# The simple test from istio/istio integration.
# Will kube-inject and test the ingress and service-to-service
run-simple:
	# Test assumes ingress is in same namespace with pilot.
	# TODO: fix test (or replace)
	bin/iop istio-control istio-ingress ${BASE}/gateways/istio-ingress --set global.istioNamespace=istio-control ${IOP_OPTS}
	kubectl wait deployments ingressgateway -n istio-control --for=condition=available --timeout=${WAIT_TIMEOUT}
	kubectl create ns simple || /bin/true
	export TMPDIR=${GOPATH}/out/tmp
	mkdir -p ${GOPATH}/out/tmp
	(cd ${GOPATH}/src/istio.io/istio; TMPDIR=${GOPATH}/out/tmp make istioctl e2e_simple_noauth_run HUB=istionightly TAG=nightly-master E2E_ARGS="--skip_setup --namespace=simple  --use_local_cluster=true --istio_namespace=istio-control")

run-integration:
	export TMPDIR=${GOPATH}/out/tmp
	mkdir -p ${GOPATH}/out/tmp
	(cd ${GOPATH}/src/istio.io/istio; TMPDIR=${GOPATH}/out/tmp make test.integration.pilot.kube)

run-stability:
	 ISTIO_ENV=istio-control bin/iop test stability ${GOPATH}/src/istio.io/tools/perf/stability/allconfig ${IOP_OPTS}

run-mysql:
	 ISTIO_ENV=istio-control bin/iop mysql mysql ${BASE}/test/mysql ${IOP_OPTS}
	 ISTIO_ENV=istio-control bin/iop mysqlplain mysqlplain ${BASE}/test/mysql ${IOP_OPTS} --set mtls=false --set Name=plain

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

# Grab kind logs to $GOPATH/out/logs
kind-logs:
	mkdir -p ${OUT}/logs
	kind export logs --name  ${KIND_CLUSTER} ${OUT}/logs

# Build the Kind+build tools image that will be useed in CI/CD or local testing
# This replaces the istio-builder.
docker.istio-builder: test/docker/Dockerfile ${GOPATH}/bin/kind ${GOPATH}/bin/helm ${GOPATH}/bin/go-junit-report ${GOPATH}/bin/repo
	mkdir -p ${GOPATH}/out/istio-builder
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
	mkdir -p $GOPATH/src/istio.io
	git clone ${GITBASE}/istio/istio.git ${GOPATH}/src/istio.io/istio

${GOPATH}/src/istio.io/tools:
	mkdir -p $GOPATH/src/istio.io
	git clone ${GITBASE}/istio/tools.git ${GOPATH}/src/istio.io/tools

#
git.dep: ${GOPATH}/src/istio.io/istio ${GOPATH}/src/istio.io/tools

${GOPATH}/bin/repo:
	curl https://storage.googleapis.com/git-repo-downloads/repo > $@
	chmod +x $@

${GOPATH}/bin/helm:
	curl -Lo - https://storage.googleapis.com/kubernetes-helm/helm-${HELM_VER}-linux-amd64.tar.gz | (cd ${GOPATH}/out; tar -zxvf -)
	chmod +x ${GOPATH}/out/linux-amd64/helm
	mv ${GOPATH}/out/linux-amd64/helm ${GOPATH}/bin/helm

${GOPATH}/bin/kind:
	echo ${GOPATH}
	go get -u sigs.k8s.io/kind

${GOPATH}/bin/dep:
	go get -u github.com/golang/dep/cmd/dep

${GOPATH}/bin/go-junit-report:
	go get github.com/jstemmer/go-junit-report
