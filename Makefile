BASE := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))

EXTRA ?= --set global.hub=${HUB} --set global.tag=${TAG}
OUT ?= ${BASE}/out


control:
	${BASE}/bin/iop istio-system citadel ${BASE}/security/citadel ${ARGS}
	# TODO: move the other components from env.sh

# Start a KIND cluster, using current docker environment, and a custom image including helm
# and additional tools to install istio.
prepare:
	kind create cluster --name test --image costinm/kind:latest

cleanup:
	-kind delete cluster --name test

# Assumes KUBECONFIG is pointing to a valid cluster.
# The cluster may be fresh ( hermetic testing ) or not
test-k8s: install-crds install-base test-bookinfo

# Install CRDS

install-crds:
	kubectl apply -f crds.yaml

wait-crds:
	kubectl wait --for=condition=Established -f crds.yaml

# Individual step to install or update base istio.
install-base:
	bin/install.sh install_system
	bin/install.sh install_control

# Individual step to run or repat a test of bookinfo
test-bookinfo:
	bin/install.sh install_ingress
	bin/install.sh install_telemetry
	# Bookinfo test
	bin/test.sh src/github.com/istio/istio


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
test: info cleanup prepare sync test-run-docker

# Target to run by default inside the builder docker image.
TEST_TARGET ?= test-k8s

# Execute a make test command in the builder docker container
test-run-docker:
	docker exec -e KUBECONFIG=/etc/kubernetes/admin.conf -w /workspace/go/src/github.com/istio-ecosystem/istio-installer \
		test-control-plane \
		make ${TEST_TARGET}

info:
	# Get info about env, for debugging
	set -x
	pwd
	env
	id


# Copy source code from the current machine to the docker.
# TODO: mount the directory in kind, run as same user (faster)
sync:
	docker exec test-control-plane mkdir -p /workspace/go/src/github.com/istio-ecosystem/istio-installer \
		/workspace/go/src/github.com/istio.io
	docker cp . test-control-plane:/workspace/go/src/github.com/istio-ecosystem/istio-installer
	docker cp ${TOP}/src/istio.io/istio test-control-plane:/workspace/go/src/istio.io
	docker cp ${TOP}/src/istio.io/tools test-control-plane:/workspace/go/src/istio.io


# Run an iterative shell in the docker image containing the build
shell-kind:
	docker exec -e KUBECONFIG=/etc/kubernetes/admin.conf -w /workspace/go/src/github.com/istio-ecosystem/istio-installer -it git \
		test-control-plane bash

# Build the Kind+build tools image that will be useed in CI/CD or local testing
# This replaces the istio-builder.
docker.istio-builder: test/docker/Dockerfile ${TOP}/bin/kind ${TOP}/bin/helm ${TOP}/bin/go-junit-report ${TOP}/bin/repo
	mkdir -p ${TOP}/out/istio-builder
	cp $^ ${TOP}/out/istio-builder/
	docker build -t costinm/kind ${TOP}/out/istio-builder

dep: ${TOP}/bin/kind ${TOP}/bin/helm

HELM_VER ?= v2.13.1

${TOP}/bin/repo:
	curl https://storage.googleapis.com/git-repo-downloads/repo > $@
	chmod +x $@

${TOP}/bin/helm:
	curl -Lo - https://storage.googleapis.com/kubernetes-helm/helm-${HELM_VER}-linux-amd64.tar.gz | (cd ${TOP}/out; tar -zxvf -)
	chmod +x ${TOP}/out/linux-amd64/helm
	mv ${TOP}/out/linux-amd64/helm ${TOP}/bin/helm

${TOP}/bin/kind:
	echo ${GOPATH}
	go get -u sigs.k8s.io/kind

${TOP}/bin/dep:
	go get -u github.com/golang/dep/cmd/dep

${TOP}/bin/go-junit-report:
	go get github.com/jstemmer/go-junit-report
