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

# If GOPATH is not set by the env, set it to a sane value
GOPATH ?= $(shell cd ${ISTIO_GO}/../../..; pwd)
export GOPATH

# If GOPATH is made up of several paths, use the first one for our targets in this Makefile
GO_TOP := $(shell echo ${GOPATH} | cut -d ':' -f1)

export CGO_ENABLE=0

# OUT is the directory where dist artifacts and temp files will be created.
OUT=${GO_TOP}/out

GO ?= go

# Compile for linux/amd64 by default.
export GOOS ?= linux
export GOARCH ?= amd64

# Optional file including user-specific settings (HUB, TAG, etc)
-include .istiorc.mk


# @todo allow user to run for a single $PKG only?
PACKAGES := $(shell $(GO) list ./...)
GO_EXCLUDE := /vendor/|.pb.go|.gen.go
GO_FILES := $(shell find . -name '*.go' | grep -v -E '$(GO_EXCLUDE)')

# Environment for tests, the directory containing istio and deps binaries.
# Typically same as GOPATH/bin, so tests work seemlessly with IDEs.
export ISTIO_BIN=${GO_TOP}/bin

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

# Discover if user has dep installed -- prefer that
DEP := $(shell which dep || echo "${ISTIO_BIN}/dep" )

# Set Google Storage bucket if not set
GS_BUCKET ?= istio-artifacts
export GS_BUCKET

#-----------------------------------------------------------------------------
# Output control
#-----------------------------------------------------------------------------
VERBOSE ?= 0
V ?= $(or $(VERBOSE),0)
Q = $(if $(filter 1,$V),,@)
H = $(shell printf "\033[34;1m=>\033[0m")

.PHONY: default
default: depend build

checkvars:
	@if test -z "$(TAG)"; then echo "TAG missing"; exit 1; fi
	@if test -z "$(HUB)"; then echo "HUB missing"; exit 1; fi

setup: pilot/platform/kube/config

#-----------------------------------------------------------------------------
# Target: depend
#-----------------------------------------------------------------------------
.PHONY: depend depend.update
.PHONY: depend.status depend.ensure depend.graph

# Pull depdendencies, based on the checked in Gopkg.lock file.
# Developers must manually call dep.update if adding new deps or to pull recent
# changes.
depend: init

depend.ensure: init

# Target to update the Gopkg.lock with latest versions.
# Should be run when adding any new dependency and periodically.
depend.update: ${DEP} ; $(info $(H) ensuring dependencies are up to date...)
	${DEP} ensure
	${DEP} ensure -update
	cp Gopkg.lock vendor/Gopkg.lock

${DEP}:
	unset GOOS && go get -u github.com/golang/dep/cmd/dep

Gopkg.lock: Gopkg.toml | ${DEP} ; $(info $(H) generating) @
	$(Q) ${DEP} ensure -update

depend.status: Gopkg.lock
	$(Q) ${DEP} status > vendor/dep.txt
	$(Q) ${DEP} status -dot > vendor/dep.dot

# Requires 'graphviz' package. Run as user
depend.view: depend.status
	cat vendor/dep.dot | dot -T png > vendor/dep.png
	display vendor/dep.pkg

lint:
	SKIP_INIT=1 bin/linters.sh

# Target run by the pre-commit script, to automate formatting and lint
# If pre-commit script is not used, please run this manually.
pre-commit: fmt lint

# Downloads envoy, based on the SHA defined in the base pilot Dockerfile
# Will also check vendor, based on Gopkg.lock
init: ${DEP}
	@(DEP=${DEP} bin/init.sh)

# init.sh downloads envoy
${ISTIO_BIN}/envoy: init

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

# gobuild script uses custom linker flag to set the variables.
# Params: OUT VERSION_PKG SRC

PILOT_GO_BINS:=${ISTIO_BIN}/pilot-discovery ${ISTIO_BIN}/pilot-agent \
               ${ISTIO_BIN}/istioctl ${ISTIO_BIN}/sidecar-initializer
$(PILOT_GO_BINS): depend
	bin/gobuild.sh ${ISTIO_BIN}/$(@F) istio.io/istio/pkg/version ./pilot/cmd/$(@F)

MIXER_GO_BINS:=${ISTIO_BIN}/mixs ${ISTIO_BIN}/mixc
$(MIXER_GO_BINS): depend
	bin/gobuild.sh ${ISTIO_BIN}/$(@F) istio.io/istio/pkg/version ./mixer/cmd/$(@F)

${ISTIO_BIN}/servicegraph: depend
	bin/gobuild.sh ${ISTIO_BIN}/servicegraph istio.io/istio/pkg/version ./mixer/example/servicegraph

SECURITY_GO_BINS:=${ISTIO_BIN}/node_agent ${ISTIO_BIN}/istio_ca
$(SECURITY_GO_BINS): depend
	bin/gobuild.sh ${ISTIO_BIN}/$(@F) istio.io/istio/pkg/version ./security/cmd/$(@F)

.PHONY: go-build
go-build: $(PILOT_GO_BINS) $(MIXER_GO_BINS) $(SECURITY_GO_BINS)

# The following are convenience aliases for most of the go targets
# The first block is for aliases that are the same as the actual binary,
# while the ones that follow need slight adjustments to their names.

IDENTITY_ALIAS_LIST:=istioctl mixc mixs pilot-agent servicegraph sidecar-initializer
.PHONY: $(IDENTITY_ALIAS_LIST)
$(foreach ITEM,$(IDENTITY_ALIAS_LIST),$(eval $(ITEM): ${ISTIO_BIN}/$(ITEM)))

.PHONY: istio-ca
istio-ca: ${ISTIO_BIN}/istio_ca

.PHONY: node-agent
node-agent: ${ISTIO_BIN}/node_agent

.PHONY: pilot
pilot: ${ISTIO_BIN}/pilot-discovery

#-----------------------------------------------------------------------------
# Target: go test
#-----------------------------------------------------------------------------

.PHONY: go-test localTestEnv test-bins

GOTEST_PARALLEL ?= '-test.parallel=4'
GOTEST_P ?= -p 1
GOSTATIC = -ldflags '-extldflags "-static"'

PILOT_TEST_BINS:=${ISTIO_BIN}/pilot-test-server ${ISTIO_BIN}/pilot-test-client ${ISTIO_BIN}/pilot-test-eurekamirror

$(PILOT_TEST_BINS): depend
	CGO_ENABLED=0 go build ${GOSTATIC} -o ${ISTIO_BIN}/$(@F) istio.io/istio/$(subst -,/,$(@F))

test-bins: $(PILOT_TEST_BINS)
	go build -o ${ISTIO_BIN}/pilot-integration-test istio.io/istio/pilot/test/integration

localTestEnv: test-bins
	bin/testEnvLocalK8S.sh ensure

# Temp. disable parallel test - flaky consul test.
# https://github.com/istio/istio/issues/2318
.PHONY: pilot-test
pilot-test: pilot-agent
	go test ${GOTEST_P} ${T} ./pilot/...

.PHONY: mixer-test
mixer-test: mixs
	# Some tests use relative path "testdata", must be run from mixer dir
	(cd mixer; go test ${T} ${GOTEST_PARALLEL} ./...)

.PHONY: broker-test
broker-test: depend
	go test ${T} ./broker/...

.PHONY: security-test
security-test:
	go test ${T} ./security/pkg/...
	go test ${T} ./security/cmd/...

common-test:
	go test ${T} ./pkg/...

# Run coverage tests
go-test: pilot-test mixer-test security-test broker-test common-test

#-----------------------------------------------------------------------------
# Target: Code coverage ( go )
#-----------------------------------------------------------------------------

.PHONY: pilot-coverage
pilot-coverage:
	bin/parallel-codecov.sh pilot

.PHONY: mixer-coverage
mixer-coverage:
	bin/parallel-codecov.sh mixer

.PHONY: broker-coverage
broker-coverage:
	bin/parallel-codecov.sh broker

.PHONY: security-coverage
security-coverage:
	bin/parallel-codecov.sh security/pkg
	bin/parallel-codecov.sh security/cmd

# Run coverage tests
coverage: pilot-coverage mixer-coverage security-coverage broker-coverage

#-----------------------------------------------------------------------------
# Target: go test -race
#-----------------------------------------------------------------------------

.PHONY: racetest

.PHONY: pilot-racetest
pilot-racetest: pilot-agent
	go test ${GOTEST_P} ${T} -race ./pilot/...

.PHONY: mixer-racetest
mixer-racetest: mixs
	# Some tests use relative path "testdata", must be run from mixer dir
	(cd mixer; go test ${T} -race ${GOTEST_PARALLEL} ./...)

.PHONY: broker-racetest
broker-racetest: depend
	go test ${T} -race ./broker/...

.PHONY: security-racetest
security-racetest:
	go test ${T} -race ./security/...

common-racetest:
	go test ${T} -race ./pkg/...

# Run race tests
racetest: pilot-racetest mixer-racetest security-racetest broker-racetest common-racetest

#-----------------------------------------------------------------------------
# Target: precommit
#-----------------------------------------------------------------------------
.PHONY: clean
.PHONY: clean.go

DIRS_TO_CLEAN:=
FILES_TO_CLEAN:=

clean: clean.go clean.installgen
	rm -rf $(DIRS_TO_CLEAN)
	rm -f $(FILES_TO_CLEAN)

clean.go: ; $(info $(H) cleaning...)
	$(eval GO_CLEAN_FLAGS := -i -r)
	$(Q) $(GO) clean $(GO_CLEAN_FLAGS)
	$(MAKE) clean -C mixer
	$(MAKE) clean -C pilot
	$(MAKE) clean -C security

test: setup go-test

# Build all prod and debug images
docker:
	time $(ISTIO_GO)/security/bin/push-docker ${hub} ${tag} -build-only
	time $(ISTIO_GO)/mixer/bin/push-docker ${hub} ${tag} -build-only
	time $(ISTIO_GO)/pilot/bin/push-docker ${hub} ${tag} -build-only

# Build docker images for pilot, mixer, ca using prebuilt binaries
docker.prebuilt:
	cp ${ISTIO_BIN}/{pilot-discovery,pilot-agent,sidecar-initializer} pilot/docker
	time (cd pilot/docker && docker build -t ${HUB}/proxy_debug:${TAG} -f Dockerfile.proxy_debug .)
	time (cd pilot/docker && docker build -t ${HUB}/proxy_init:${TAG} -f Dockerfile.proxy_init .)
	time (cd pilot/docker && docker build -t ${HUB}/sidecar_initializer:${TAG} -f Dockerfile.sidecar_initializer .)
	time (cd pilot/docker && docker build -t ${HUB}/pilot:${TAG} -f Dockerfile.pilot .)
	cp ${ISTIO_BIN}/mixs mixer/docker
	cp docker/ca-certificates.tgz mixer/docker
	time (cd mixer/docker && docker build -t ${HUB}/mixer_debug:${TAG} -f Dockerfile.debug .)
	cp ${ISTIO_BIN}/{istio_ca,node_agent} security/docker
	cp docker/ca-certificates.tgz security/docker/
	time (cd security/docker && docker build -t ${HUB}/istio-ca:${TAG} -f Dockerfile.istio-ca .)
	time (cd security/docker && docker build -t ${HUB}/node-agent:${TAG} -f Dockerfile.node-agent .)
	cp ${ISTIO_BIN}/{pilot-test-client,pilot-test-server,pilot-test-eurekamirror} pilot/docker
	time (cd pilot/docker && docker build -t ${HUB}/app:${TAG} -f Dockerfile.app .)
	time (cd pilot/docker && docker build -t ${HUB}/eurekamirror:${TAG} -f Dockerfile.eurekamirror .)
	# TODO: generate or checkin test CA and keys
	## These are not used so far
	security/bin/gen-keys.sh
	time (cd security/docker && docker build -t ${HUB}/istio-ca-test:${TAG} -f Dockerfile.istio-ca-test .)
	time (cd security/docker && docker build -t ${HUB}/node-agent-test:${TAG} -f Dockerfile.node-agent-test .)

##################################################################################

# static files that are already at proper location for docker build

PROXY_JSON_FILES:=pilot/docker/envoy_pilot.json \
                  pilot/docker/envoy_pilot_auth.json \
                  pilot/docker/envoy_mixer.json \
                  pilot/docker/envoy_mixer_auth.json

NODE_AGENT_TEST_FILES:=security/docker/start_app.sh \
                       security/docker/app.js \
                       security/docker/istio_ca.crt \
                       security/docker/node_agent.crt \
                       security/docker/node_agent.key

# copied/generated files for docker build

.SECONDEXPANSION: #allow $@ to be used in dependency list

# each of these files .../XXX is copied from ${ISTIO_BIN}/XXX
# NOTE: each of these are passed to rm -f during "make clean".  Keep in mind
# cases where you might change any of these to be a new or former source code path
COPIED_FROM_ISTIO_BIN:=pilot/docker/pilot-agent pilot/docker/pilot-discovery \
                       pilot/docker/pilot-test-client pilot/docker/pilot-test-server \
                       pilot/docker/sidecar-initializer pilot/docker/pilot-test-eurekamirror \
                       mixer/docker/mixs mixer/example/servicegraph/docker/servicegraph \
                       security/docker/istio_ca security/docker/node_agent

$(COPIED_FROM_ISTIO_BIN): ${ISTIO_BIN}/$$(@F)
	cp $< $(@D)

FILES_TO_CLEAN+=$(COPIED_FROM_ISTIO_BIN)

# this rule is an exxeption since "viz" is a directory rather than a file
mixer/example/servicegraph/docker/viz: mixer/example/servicegraph/js/viz
	cp -r $< $(@D)

DIRS_TO_CLEAN+=mixer/example/servicegraph/docker/viz

mixer/docker/ca-certificates.tgz security/docker/ca-certificates.tgz: docker/ca-certificates.tgz
	cp $< $(@D)

FILES_TO_CLEAN+=mixer/docker/ca-certificates.tgz security/docker/ca-certificates.tgz

# NOTE: this list is passed to rm -f during "make clean"
GENERATED_CERT_FILES:=security/docker/istio_ca.crt security/docker/istio_ca.key \
                      security/docker/node_agent.crt security/docker/node_agent.key

$(GENERATED_CERT_FILES): security/bin/gen-keys.sh
	security/bin/gen-keys.sh

FILES_TO_CLEAN+=$(GENERATED_CERT_FILES)

# pilot docker images

docker.app: pilot/docker/pilot-test-client pilot/docker/pilot-test-server \
            pilot/docker/certs/cert.crt pilot/docker/certs/cert.key
docker.eurekamirror: pilot/docker/pilot-test-eurekamirror
docker.pilot: pilot/docker/pilot-discovery
docker.proxy docker.proxy_debug: pilot/docker/pilot-agent ${PROXY_JSON_FILES}
docker.proxy_init: pilot/docker/prepare_proxy.sh
docker.sidecar_initializer: pilot/docker/sidecar-initializer

PILOT_DOCKER:=docker.app docker.eurekamirror docker.pilot docker.proxy \
              docker.proxy_debug docker.proxy_init docker.sidecar_initializer
$(PILOT_DOCKER): pilot/docker/Dockerfile$$(suffix $$@)
	time (cd pilot/docker && docker build -t $(subst docker.,,$@) -f Dockerfile$(suffix $@) .)

# mixer/example docker images

SERVICEGRAPH_DOCKER:=docker.servicegraph docker.servicegraph_debug
$(SERVICEGRAPH_DOCKER): mixer/example/servicegraph/docker/Dockerfile$$(if $$(findstring debug,$$@),.debug) \
		mixer/example/servicegraph/docker/servicegraph mixer/example/servicegraph/docker/viz
	time (cd mixer/example/servicegraph/docker && docker build -t servicegraph$(findstring _debug,$@) \
		-f Dockerfile$(if $(findstring debug,$@),.debug) .)

# mixer docker images

MIXER_DOCKER:=docker.mixer docker.mixer_debug
$(MIXER_DOCKER): mixer/docker/Dockerfile$$(if $$(findstring debug,$$@),.debug) \
		mixer/docker/ca-certificates.tgz mixer/docker/mixs
	time (cd mixer/docker && docker build -t mixer$(findstring _debug,$@) \
		-f Dockerfile$(if $(findstring debug,$@),.debug) .)

# security docker images

docker.istio-ca: security/docker/istio_ca security/docker/ca-certificates.tgz
docker.istio-ca-test: security/docker/istio_ca.crt security/docker/istio_ca.key
docker.node-agent:      security/docker/node_agent
docker.node-agent-test: security/docker/node_agent ${NODE_AGENT_TEST_FILES}

SECURITY_DOCKER:=docker.istio-ca docker.istio-ca-test docker.node-agent docker.node-agent-test
$(SECURITY_DOCKER): security/docker/Dockerfile$$(suffix $$@)
	time (cd security/docker && docker build -t $(subst docker.,,$@) -f Dockerfile$(suffix $@) .)

DOCKER_TARGETS:=$(PILOT_DOCKER) $(SERVICEGRAPH_DOCKER) $(MIXER_DOCKER) $(SECURITY_DOCKER)

docker.all: $(DOCKER_TARGETS)

# for each docker.XXX target create a tar.docker.XXX target that says how
# to make a $(OUT)/docker/XXX.tar.gz from the docker XXX image
# note that $(subst docker.,,$(TGT)) strips off the "docker." prefix, leaving just the XXX
$(foreach TGT,$(DOCKER_TARGETS),$(eval tar.$(TGT): $(TGT); \
   time (mkdir -p ${OUT}/docker && \
         docker save -o ${OUT}/docker/$(subst docker.,,$(TGT)).tar $(subst docker.,,$(TGT)) && \
         gzip ${OUT}/docker/$(subst docker.,,$(TGT)).tar)))

# create a DOCKER_TAR_TARGETS that's each of DOCKER_TARGETS with a tar. prefix
DOCKER_TAR_TARGETS:=
$(foreach TGT,$(DOCKER_TARGETS),$(eval DOCKER_TAR_TARGETS+=tar.$(TGT)))

# this target saves a tar.gz of each docker image to ${OUT}/docker/
docker.save: $(DOCKER_TAR_TARGETS)

push: checkvars clean.installgen installgen
	$(ISTIO_GO)/bin/push $(HUB) $(TAG) $(GS_BUCKET)

artifacts: docker
	@echo 'To be added'

pilot/platform/kube/config:
	touch $@

kubelink:
	ln -fs ~/.kube/config pilot/platform/kube/

installgen:
	install/updateVersion.sh -a ${HUB},${TAG}

clean.installgen:
	rm -f install/consul/istio.yaml
	rm -f install/eureka/istio.yaml
	rm -f install/kubernetes/helm/istio/values.yaml
	rm -f install/kubernetes/istio-auth.yaml
	rm -f install/kubernetes/istio-ca-plugin-certs.yaml
	rm -f install/kubernetes/istio-initializer.yaml
	rm -f install/kubernetes/istio-one-namespace-auth.yaml
	rm -f install/kubernetes/istio-one-namespace.yaml
	rm -f install/kubernetes/istio.yaml
	rm -f pilot/docker/pilot-test-client
	rm -f pilot/docker/pilot-test-eurekamirror
	rm -f pilot/docker/pilot-test-server
	rm -f samples/bookinfo/consul/bookinfo.sidecars.yaml
	rm -f samples/bookinfo/eureka/bookinfo.sidecars.yaml
	rm -f security/docker/ca-certificates.tgz

.PHONY: artifacts build checkvars clean docker test setup push kubelink installgen clean.installgen

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

# Building the debian file, docker.istio.deb and istio.deb
include tools/deb/istio.mk

#-----------------------------------------------------------------------------
# Target: e2e tests
#-----------------------------------------------------------------------------
include tests/istio.mk
