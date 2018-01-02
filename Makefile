## Copyright 2017 Istio Authors
##
## Licensed under the Apache License, Version 2.0 (the "License");
## you may not use this file except in compliance with the License.
## You may obtain a copy of the License at
##
##     http://www.apache.org/licenses/LICENSE-2.0
##
## Unless required by applicable law or agreed to in writing, software
## distributed under the License is distributed on an "AS IS" BASIS,
## WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
## See the License for the specific language governing permissions and
## limitations under the License.

#-----------------------------------------------------------------------------
# Global Variables
#-----------------------------------------------------------------------------
ISTIO_GO := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
SHELL := /bin/bash

# Current version, updated after a release.
VERSION ?= "0.5.0"

# Make sure GOPATH is set based on the executing Makefile and workspace. Will override
# GOPATH from the env.
export GOPATH= $(shell cd ../../..; pwd)

export CGO_ENABLE=0

# OUT is the directory where dist artifacts and temp files will be created.
OUT=${GOPATH}/out

GO ?= go

# Compile for linux/amd64 by default.
export GOOS ?= linux
export GOARCH ?= amd64

# Optional file including user-specific settings (HUB, TAG, etc)
-include .istiorc


# @todo allow user to run for a single $PKG only?
PACKAGES := $(shell $(GO) list ./...)
GO_EXCLUDE := /vendor/|.pb.go|.gen.go
GO_FILES := $(shell find . -name '*.go' | grep -v -E '$(GO_EXCLUDE)')

# Environment for tests, the directory containing istio and deps binaries.
# Typically same as GOPATH/bin, so tests work seemlessly with IDEs.
export ISTIO_BIN=${GOPATH}/bin

hub = ""
tag = ""

HUB?=istio
ifneq ($(strip $(HUB)),)
	hub =-hub ${HUB}
endif

# If tag not explicitly set in users' .istiorc or command line, default to the git sha.
TAG ?= $(shell git rev-parse --verify HEAD)
export TAG
export HUB
ifneq ($(strip $(TAG)),)
	tag =-tag ${TAG}
endif

#-----------------------------------------------------------------------------
# Output control
#-----------------------------------------------------------------------------
VERBOSE ?= 0
V ?= $(or $(VERBOSE),0)
Q = $(if $(filter 1,$V),,@)
H = $(shell printf "\033[34;1m=>\033[0m")

.DEFAULT_GOAL := build

checkvars:
	@if test -z "$(TAG)"; then echo "TAG missing"; exit 1; fi
	@if test -z "$(HUB)"; then echo "HUB missing"; exit 1; fi

setup: pilot/platform/kube/config

#-----------------------------------------------------------------------------
# Target: depend
#-----------------------------------------------------------------------------
.PHONY: depend 
.PHONY: depend.status depend.ensure depend.graph

depend: depend.ensure

${GOPATH}/bin/dep:
	go get -u github.com/golang/dep/cmd/dep

Gopkg.lock: Gopkg.toml ; $(info $(H) generating) @
	$(Q) dep ensure -update

depend.status: Gopkg.lock ; $(info $(H) reporting dependencies status...)
	$(Q) dep status

# @todo only run if there are changes (e.g., create a checksum file?) 
# Update the vendor dir, pulling latest compatible dependencies from the
# defined branches.
depend.ensure: Gopkg.lock ${GOPATH}/bin/dep; $(info $(H) ensuring dependencies are up to date...)
	$(Q) dep ensure

depend.graph: Gopkg.lock ; $(info $(H) visualizing dependency graph...)
	$(Q) dep status -dot | dot -T png | display

# Re-create the vendor directory, if it doesn't exist, using the checked in lock file
depend.vendor: vendor
	$(Q) dep ensure -vendor-only

vendor:
	dep ensure -update

lint:
	SKIP_INIT=1 bin/linters.sh

# Target run by the pre-commit script, to automate formatting and lint
# If pre-commit script is not used, please run this manually.
pre-commit: fmt lint

#-----------------------------------------------------------------------------
# Target: precommit
#-----------------------------------------------------------------------------
.PHONY: precommit format check
.PHONY: fmt format.gofmt format.goimports
.PHONY: check.vet check.lint

precommit: format check
format: format.goimports
fmt: format.gofmt format.goimports # backward compatible with ./bin/fmt.sh
check: check.vet check.lint

format.gofmt: ; $(info $(H) formatting files with go fmt...)
	$(Q) gofmt -s -w $(GO_FILES)

format.goimports: ; $(info $(H) formatting files with goimports...)
	$(Q) goimports -w -local istio.io $(GO_FILES)

# @todo fail on vet errors? Currently uses `true` to avoid aborting on failure
check.vet: ; $(info $(H) running go vet on packages...)
	$(Q) $(GO) vet $(PACKAGES) || true

# @todo fail on lint errors? Currently uses `true` to avoid aborting on failure
# @todo remove _test and mock_ from ignore list and fix the errors?
check.lint: ; $(info $(H) running golint on packages...)
	$(eval LINT_EXCLUDE := $(GO_EXCLUDE)|_test.go|mock_)
	$(Q) for p in $(PACKAGES); do \
		golint $$p | grep -v -E '$(LINT_EXCLUDE)' ; \
	done || true;

# @todo gometalinter targets?

build: setup go-build

#-----------------------------------------------------------------------------
# Target: go build
#-----------------------------------------------------------------------------

.PHONY: go-build

# gobuild script uses custom linker flag to set the variables.
# Params: OUT VERSION_PKG SRC

.PHONY: pilot
pilot: vendor
	bin/gobuild.sh ${GOPATH}/bin/pilot-discovery istio.io/istio/pilot/tools/version ./pilot/cmd/pilot-discovery

.PHONY: pilot-agent
pilot-agent: vendor
	bin/gobuild.sh ${GOPATH}/bin/pilot-agent istio.io/istio/pilot/tools/version ./pilot/cmd/pilot-agent

.PHONY: istioctl
istioctl: vendor
	bin/gobuild.sh ${GOPATH}/bin/istioctl istio.io/istio/pilot/tools/version ./pilot/cmd/istioctl

.PHONY: sidecar-initializer
sidecar-initializer: vendor
	bin/gobuild.sh ${GOPATH}/bin/sidecar-initializer istio.io/istio/pilot/tools/version ./pilot/cmd/sidecar-initializer

.PHONY: mixs
mixs: vendor
	bin/gobuild.sh ${GOPATH}/bin/mixs istio.io/istio/mixer/pkg/version ./mixer/cmd/mixs

.PHONY: mixc
mixc: vendor
	go install istio.io/istio/mixer/cmd/mixc

.PHONY: node-agent
node-agent: vendor
	bin/gobuild.sh ${GOPATH}/bin/node_agent istio.io/istio/security/cmd/istio_ca/version ./security/cmd/node_agent

.PHONY: istio-ca
istio-ca: vendor
	bin/gobuild.sh ${GOPATH}/bin/istio_ca istio.io/istio/security/cmd/istio_ca/version ./security/cmd/istio_ca

go-build: pilot istioctl pilot-agent sidecar-initializer mixs mixc node-agent istio-ca

#-----------------------------------------------------------------------------
# Target: go test
#-----------------------------------------------------------------------------

.PHONY: go-test localTestEnv

GOTEST_PARALLEL ?= '-test.parallel=4'

localTestEnv:
	bin/testEnvLocalK8S.sh ensure
	go install istio.io/istio/pilot/test/server
	go install istio.io/istio/pilot/test/client
	go install istio.io/istio/pilot/test/eurekamirror

# Temp. disable parallel test - flaky consul test.
# https://github.com/istio/istio/issues/2318
.PHONY: pilot-test
pilot-test: pilot-agent
	go test ${T} ./pilot/...

.PHONY: mixer-test
mixer-test: mixs
	# Some tests use relative path "testdata", must be run from mixer dir
	(cd mixer; go test ${T} ${GOTEST_PARALLEL} ./...)

.PHONY: broker-test
broker-test: vendor
	go test ${T} ./broker/...

.PHONY: security-test
security-test:
	go test ${T} ./security/...

# Run coverage tests
go-test: pilot-test mixer-test security-test broker-test

#-----------------------------------------------------------------------------
# Target: Code coverage ( go )
#-----------------------------------------------------------------------------

.PHONY: pilot-cov
pilot-cov:
	bin/parallel-codecov.sh pilot

.PHONY: mixer-cov
mixer-cov:
	bin/parallel-codecov.sh mixer

.PHONY: broker-cov
broker-cov:
	bin/parallel-codecov.sh broker

.PHONY: security-cov
security-cov:
	bin/parallel-codecov.sh security

# Run coverage tests
cov: pilot-cov mixer-cov security-cov broker-cov


#-----------------------------------------------------------------------------
# Target: precommit
#-----------------------------------------------------------------------------
.PHONY: clean
.PHONY: clean.go

clean: clean.go

clean.go: ; $(info $(H) cleaning...)
	$(eval GO_CLEAN_FLAGS := -i -r)
	$(Q) $(GO) clean $(GO_CLEAN_FLAGS)
	$(MAKE) clean -C mixer
	$(MAKE) clean -C pilot
	$(MAKE) clean -C security

test: setup go-test

docker:
	$(ISTIO_GO)/security/bin/push-docker ${hub} ${tag} -build-only
	$(ISTIO_GO)/mixer/bin/push-docker ${hub} ${tag} -build-only
	$(ISTIO_GO)/pilot/bin/push-docker ${hub} ${tag} -build-only

push: checkvars
	$(ISTIO_GO)/bin/push $(HUB) $(TAG)

artifacts: docker
	@echo 'To be added'

pilot/platform/kube/config:
	touch $@

kubelink:
	ln -fs ~/.kube/config pilot/platform/kube/

.PHONY: artifacts build checkvars clean docker test setup push kubelink

#-----------------------------------------------------------------------------
# Target: environment and tools
#-----------------------------------------------------------------------------
.PHONY: show.env show.goenv

show.env: ; $(info $(H) environment variables...)
	$(Q) printenv

show.goenv: ; $(info $(H) go environment...)
	$(Q) $(GO) version
	$(Q) $(GO) env

# show makefile variables. Usage: make show.<variable-name>
show.%: ; $(info $* $(H) $($*))
	$(Q) true

#-----------------------------------------------------------------------------
# Target: artifacts and distribution
#-----------------------------------------------------------------------------


${OUT}/dist/Gopkg.lock:
	mkdir -p ${OUT}/dist
	cp Gopkg.lock ${OUT}/dist/

# Binary/built artifacts of the distribution
dist-bin: ${OUT}/dist/Gopkg.lock

dist: dist-bin

include .circleci/Makefile

.PHONY: docker.sidecar.deb sidecar.deb

# Make the deb image using the CI/CD image and docker.
docker.sidecar.deb:
	(cd ${TOP}; docker run --rm -u $(shell id -u) -it \
        -v ${GOPATH}:${GOPATH} \
        -w ${PWD} \
        -e USER=${USER} \
		--entrypoint /usr/bin/make ${CI_HUB}/ci:${CI_VERSION} \
		sidecar.deb )


# Create the 'sidecar' deb, including envoy and istio agents and configs.
# This target uses a locally installed 'fpm' - use 'docker.sidecar.deb' to use
# the builder image.
# TODO: consistent layout, possibly /opt/istio-VER/...
sidecar.deb:
	fpm -s dir -t deb -n istio-sidecar --version ${VERSION} --iteration 1 -C ${GOPATH} -f \
	   --url http://istio.io  \
	   --license Apache \
	   --vendor istio.io \
	   --maintainer istio@istio.io \
	   --after-install tools/deb/postinst.sh \
	   --config-files /var/lib/istio/envoy/sidecar.env \
	   --config-files /var/lib/istio/envoy/envoy.json \
	   --description "Istio" \
	   src/istio.io/istio/tools/deb/istio-start.sh=/usr/local/bin/istio-start.sh \
	   src/istio.io/istio/tools/deb/istio-iptables.sh=/usr/local/bin/istio-iptables/sh \
	   src/istio.io/istio/tools/deb/istio.service=/lib/systemd/system/istio.service \
	   src/istio.io/istio/security/tools/deb/istio-auth-node-agent.service=/lib/systemd/system/istio-auth-node-agent.service \
	   bin/envoy=/usr/local/bin/envoy \
	   bin/pilot-agent=/usr/local/bin/pilot-agent \
	   bin/node_agent=/usr/local/istio/bin/node_agent \
	   src/istio.io/istio/tools/deb/sidecar.env=/var/lib/istio/envoy/sidecar.env \
	   src/istio.io/istio/tools/deb/envoy.json=/var/lib/istio/envoy/envoy.json


#-----------------------------------------------------------------------------
# Target: e2e tests
#-----------------------------------------------------------------------------
include tests/istio.mk

