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

BASE := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
GOPATH = $(shell cd ${BASE}/../../../..; pwd)
TOP ?= $(GOPATH)

EXTRA ?= --set global.hub=${HUB} --set global.tag=${TAG}
OUT ?= ${BASE}/out

# Target to run by default inside the builder docker image.
TEST_TARGET ?= test-k8s

ifeq ($(MOUNT), undefined)
	KIND_CONFIG = ""
else
	KIND_CONFIG ?= "--config ${GOPATH}/kind.yaml"
endif

control:
	${BASE}/bin/iop istio-system citadel ${BASE}/security/citadel ${ARGS}
	# TODO: move the other components from env.sh

# Start a KIND cluster, using current docker environment, and a custom image including helm
# and additional tools to install istio.
prepare:
	cat test/kind/kind.yaml | sed s/GOPATH/$(GOPATH) > ${GOPATH}/kind.yaml
	kind create cluster --name test --wait 60s ${KIND_CONFIG} --image costinm/kind:latest

clean:
	-kind delete cluster --name test

# Assumes KUBECONFIG is pointing to a valid cluster.
# The cluster may be fresh ( hermetic testing ) or not
test-k8s: install-crds install-base test-bookinfo

# Install CRDS

install-crds:
	kubectl apply -f crds.yaml

wait-crds:
	kubectl wait --for=condition=Established -f crds.yaml

WAIT_TIMEOUT ?= 30s

IOP_OPTS="-f test/kind/user-values.yaml"

# Individual step to install or update base istio.
install-base:
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

run-bookinfo:
	echo ${BASE} ${GOPATH}
	# Bookinfo test
	SKIP_CLEANUP=1 bin/test.sh ${GOPATH}/src/istio.io/istio

# Individual step to run or repat a test of bookinfo
test-bookinfo: install-base install-ingress install-telemetry run-bookinfo


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
# 3. make sync ( to copy modified files )
# 4. make test-run-docker
test: info clean prepare sync test-run-docker

# Execute a make test command in the builder docker container
test-run-docker:
	docker exec -e KUBECONFIG=/etc/kubernetes/admin.conf -w ${GOPATH}/go/src/github.com/istio-ecosystem/istio-installer \
		test-control-plane \
		make ${TEST_TARGET}

info:
	# Get info about env, for debugging
	set -x
	pwd
	env
	id


# Copy source code from the current machine to the docker.
sync:
ifeq ($(MOUNT), undefined)
	docker exec test-control-plane mkdir -p ${GOPATH}/src/github.com/istio-ecosystem/istio-installer \
		${GOPATH}/src/github.com/istio.io
	docker cp . test-control-plane:${GOPATH}/go/src/github.com/istio-ecosystem/istio-installer
	docker cp ${GOPATH}/src/istio.io/istio test-control-plane:${GOPATH}/go/src/istio.io
	docker cp ${GOPATH}/src/istio.io/tools test-control-plane:${GOPATH}/go/src/istio.io
endif

# Run an iterative shell in the docker image containing the build
shell-kind:
ifeq ($(MOUNT), undefined)
	docker exec -it -e KUBECONFIG=/etc/kubernetes/admin.conf \
		-w ${GOPATH}/src/github.com/istio-ecosystem/istio-installer \
		test-control-plane bash
else
	docker exec test-control-plane bash -c "cp /etc/kubernetes/admin.conf /tmp && chown $(shell id -u) /tmp/admin.conf"
	docker exec -it -e KUBECONFIG=/tmp/admin.conf \
		-u "$(shell id -u)" -e USER="${USER}" -e HOME="${GOPATH}" \
		-w ${GOPATH}/src/github.com/istio-ecosystem/istio-installer \
		test-control-plane bash
endif

user-shell-kind:
	echo ${GOPATH}
	docker exec -it -e KUBECONFIG=/etc/kubernetes/admin.conf \
		test-control-plane bash

# Build the Kind+build tools image that will be useed in CI/CD or local testing
# This replaces the istio-builder.
docker.istio-builder: test/docker/Dockerfile ${TOP}/bin/kind ${TOP}/bin/helm ${TOP}/bin/go-junit-report ${TOP}/bin/repo
	mkdir -p ${GOPATH}/out/istio-builder
	cp $^ ${GOPATH}/out/istio-builder/
	docker build -t costinm/kind ${GOPATH}/out/istio-builder

dep: ${GOPATH}/bin/kind ${GOPATH}/bin/helm

HELM_VER ?= v2.13.1

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
