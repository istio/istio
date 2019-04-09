BASE := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))

EXTRA ?= --set global.hub=${HUB} --set global.tag=${TAG}
OUT ?= ${BASE}/out


control:
	${BASE}/bin/iop istio-system citadel ${BASE}/security/citadel ${ARGS}
	# TODO: move the other components from env.sh

prepare:
	kind create cluster --name test

cleanup:
	kind delete cluster --name test

# Assumes KUBECONFIG is pointing to a valid cluster.
# The cluster may be fresh ( hermetic testing ) or not
test-k8s: install-crds install-base test-bookinfo

# Install CRDS

install-crds:
	kubectl apply -f crds.yaml

wait-crds:
	kubectl wait --for=condition=Established -f crds.yaml

install-base:
	bin/install.sh install_system
	bin/install.sh install_control

test-bookinfo:
	bin/install.sh install_ingress
	bin/install.sh install_telemetry
	# Bookinfo test
	bin/test.sh src/github.com/istio/istio



# Steps to run in a CI/CD system capable of Docker priv execution
# Assumes the installer is checked out, and is the current working directory.
ci-kind: info prepare sync
	docker exec -e KUBECONFIG=/etc/kubernetes/admin.conf -w /workspace/go/src/github.com/istio-ecosystem/istio-installer \
		test-control-plane \
		make test-k8s

info:
	# Get info about env, for debugging
	set -x
	pwd
	env
	id


sync:
	docker exec test-control-plane mkdir -p /workspace/go/src/github.com/istio-ecosystem/istio-installer
	docker cp . test-control-plane:/workspace/go/src/github.com/istio-ecosystem/istio-installer


shell-kind:
	docker exec -e KUBECONFIG=/etc/kubernetes/admin.conf -w /workspace/go/src/github.com/istio-ecosystem/istio-installer -it git \
		test-control-plane bash

docker.istio-builder: ${TOP}/bin/kind ${TOP}/bin/helm ${TOP}/bin/go-junit-report
	mkdir -p ${TOP}/out/istio-builder
	cp test/docker/Dockerfile ${TOP}/bin/kind ${TOP}/bin/helm ${TOP}/bin/go-junit-report\
		${TOP}/out/istio-builder/
	docker build -t costinm/kind ${TOP}/out/istio-builder

dep: ${TOP}/bin/kind ${TOP}/bin/helm

HELM_VER ?= v2.13.1

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
