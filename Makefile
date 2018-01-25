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
VERSION ?= 0.5.0

# locations where artifacts are stored
ISTIO_DOCKER_HUB ?= docker.io/istio
ISTIO_GCS ?= istio-release/releases/$(VERSION)
ISTIO_URL ?= https://storage.googleapis.com/$(ISTIO_GCS)
ISTIO_URL_ISTIOCTL ?= istioctl

# If GOPATH is not set by the env, set it to a sane value
GOPATH ?= $(shell cd ${ISTIO_GO}/../../..; pwd)
export GOPATH

# If GOPATH is made up of several paths, use the first one for our targets in this Makefile
GO_TOP := $(shell echo ${GOPATH} | cut -d ':' -f1)

# Note that disabling cgo here adversely affects go get.  Instead we'll rely on this
# to be handled in bin/gobuild.sh
# export CGO_ENABLED=0

# It's more concise to use GO?=$(shell which go)
# but the following approach uses a more efficient "simply expanded" :=
# variable instead of a "recursively expanded" =
ifeq ($(origin GO), undefined)
  GO:=$(shell which go)
endif
ifeq ($(GO),)
  $(error Could not find 'go' in path.  Please install go, or if already installed either add it to your path or set GO to point to its directory)
endif

export GOARCH ?= amd64

LOCAL_OS := $(shell uname)
ifeq ($(LOCAL_OS),Linux)
   export GOOS ?= linux
else ifeq ($(LOCAL_OS),Darwin)
   export GOOS ?= darwin
else
   $(error "This system's OS $(LOCAL_OS) isn't recognized/supported")
   # export GOOS ?= windows
endif

ifeq ($(GOOS),linux)
  OS_DIR:=lx
else ifeq ($(GOOS),darwin)
  OS_DIR:=mac
else ifeq ($(GOOS),windows)
  OS_DIR:=win
else
   $(error "Building for $(GOOS) isn't recognized/supported")
endif

# Another person's PR is adding the debug support, so this is in prep for that
ifeq ($(DEBUG),0)
BUILDTYPE_DIR:=release
else
BUILDTYPE_DIR:=debug
endif

# Optional file including user-specific settings (HUB, TAG, etc)
-include .istiorc.mk

# @todo allow user to run for a single $PKG only?
PACKAGES_CMD := GOPATH=$(GOPATH) $(GO) list ./...
GO_EXCLUDE := /vendor/|.pb.go|.gen.go
GO_FILES_CMD := find . -name '*.go' | grep -v -E '$(GO_EXCLUDE)'

# Environment for tests, the directory containing istio and deps binaries.
# Typically same as GOPATH/bin, so tests work seemlessly with IDEs.

export ISTIO_BIN=$(GO_TOP)/bin
export ISTIO_OUT:=$(GO_TOP)/out/$(OS_DIR)/$(GOARCH)/$(BUILDTYPE_DIR)
# this shouldn't be simply 'docker' since that's used for docker.save to store tar.gz files
ISTIO_DOCKER:=${ISTIO_OUT}/docker_temp

GO_VERSION_REQUIRED:=1.9

# Parse out the x.y or x.y.z version and output a single value x*10000+y*100+z (e.g., 1.9 is 10900)
# that allows the three components to be checked in a single comparison.
VER_TO_INT:=awk '{split(substr($$0, match ($$0, /[0-9\.]+/)), a, "."); print a[1]*10000+a[2]*100+a[3]}'

# using a sentinel file so this check is only performed once per version.  Performance is
# being favored over the unlikely situation that go gets downgraded to an older version
check-go-version: | $(ISTIO_BIN) ${ISTIO_BIN}/have_go_$(GO_VERSION_REQUIRED)
${ISTIO_BIN}/have_go_$(GO_VERSION_REQUIRED):
	@if test $(shell $(GO) version | $(VER_TO_INT) ) -lt \
                 $(shell echo "$(GO_VERSION_REQUIRED)" | $(VER_TO_INT) ); \
                 then printf "go version $(GO_VERSION_REQUIRED)+ required, found: "; $(GO) version; exit 1; fi
	@touch ${ISTIO_BIN}/have_go_$(GO_VERSION_REQUIRED)

HUB?=istio
ifeq ($(HUB),)
  $(error "HUB cannot be empty")
endif

# If tag not explicitly set in users' .istiorc or command line, default to the git sha.
TAG ?= $(shell git rev-parse --verify HEAD)
ifeq ($(TAG),)
  $(error "TAG cannot be empty")
endif

# Discover if user has dep installed -- prefer that
DEP    := $(shell which dep    || echo "${ISTIO_BIN}/dep" )
GOLINT := $(shell which golint || echo "${ISTIO_BIN}/golint" )

# Set Google Storage bucket if not set
GS_BUCKET ?= istio-artifacts

#-----------------------------------------------------------------------------
# Output control
#-----------------------------------------------------------------------------
VERBOSE ?= 0
V ?= $(or $(VERBOSE),0)
Q = $(if $(filter 1,$V),,@)
H = $(shell printf "\033[34;1m=>\033[0m")

.PHONY: default
default: depend build

setup:

#-----------------------------------------------------------------------------
# Target: depend
#-----------------------------------------------------------------------------
.PHONY: depend depend.update
.PHONY: depend.status depend.ensure depend.graph

# Pull depdendencies, based on the checked in Gopkg.lock file.
# Developers must manually call dep.update if adding new deps or to pull recent
# changes.
depend: init $(ISTIO_OUT)

$(ISTIO_OUT) $(ISTIO_BIN):
	mkdir -p $@

depend.ensure: init

# Target to update the Gopkg.lock with latest versions.
# Should be run when adding any new dependency and periodically.
depend.update: ${DEP} ; $(info $(H) ensuring dependencies are up to date...)
	${DEP} ensure
	${DEP} ensure -update
	cp Gopkg.lock vendor/Gopkg.lock

# If CGO_ENABLED=0 then go get tries to install in system directories.
# If -pkgdir <dir> is also used then various additional .a files are present.
${DEP}:
	unset GOOS && CGO_ENABLED=1 go get -u github.com/golang/dep/cmd/dep

${GOLINT}:
	unset GOOS && CGO_ENABLED=1 go get -u github.com/golang/lint/golint

Gopkg.lock: Gopkg.toml | ${DEP} ; $(info $(H) generating) @
	$(Q) ${DEP} ensure -update

depend.status: Gopkg.lock
	$(Q) ${DEP} status > vendor/dep.txt
	$(Q) ${DEP} status -dot > vendor/dep.dot

# Requires 'graphviz' package. Run as user
depend.view: depend.status
	cat vendor/dep.dot | dot -T png > vendor/dep.png
	display vendor/dep.pkg

# Existence of build cache .a files actually affects the results of
# some linters; they need to exist.
lint: buildcache
	SKIP_INIT=1 bin/linters.sh

# Build with -i to store the build caches into $GOPATH/pkg
buildcache:
	GOBUILDFLAGS=-i $(MAKE) build

.PHONY: buildcache

# Target run by the pre-commit script, to automate formatting and lint
# If pre-commit script is not used, please run this manually.
pre-commit: fmt lint

# Downloads envoy, based on the SHA defined in the base pilot Dockerfile
# Will also check vendor, based on Gopkg.lock
init: $(ISTIO_BIN)/istio_is_init

$(ISTIO_BIN)/istio_is_init: check-go-version bin/init.sh pilot/docker/Dockerfile.proxy_debug | ${DEP}
	@(DEP=${DEP} ISTIO_OUT=${ISTIO_OUT} bin/init.sh)
	touch $(ISTIO_BIN)/istio_is_init

# init.sh downloads envoy
${ISTIO_OUT}/envoy: init

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
	$(Q) gofmt -s -w $$($(GO_FILES_CMD))

format.goimports: ; $(info $(H) formatting files with goimports...)
	$(Q) goimports -w -local istio.io $$($(GO_FILES_CMD))

# @todo fail on vet errors? Currently uses `true` to avoid aborting on failure
check.vet: ; $(info $(H) running go vet on packages...)
	$(Q) $(GO) vet $$($(PACKAGES_CMD)) || true

# @todo fail on lint errors? Currently uses `true` to avoid aborting on failure
# @todo remove _test and mock_ from ignore list and fix the errors?
check.lint: | ${GOLINT} ; $(info $(H) running golint on packages...)
	$(eval LINT_EXCLUDE := $(GO_EXCLUDE)|_test.go|mock_)
	$(Q) for p in $$($(PACKAGES_CMD)); do \
		${GOLINT} $$p | grep -v -E '$(LINT_EXCLUDE)' ; \
	done || true;

# @todo gometalinter targets?

build: setup go-build

#-----------------------------------------------------------------------------
# Target: go build
#-----------------------------------------------------------------------------

# gobuild script uses custom linker flag to set the variables.
# Params: OUT VERSION_PKG SRC

PILOT_GO_BINS:=${ISTIO_OUT}/pilot-discovery ${ISTIO_OUT}/pilot-agent \
               ${ISTIO_OUT}/istioctl ${ISTIO_OUT}/sidecar-injector
$(PILOT_GO_BINS): depend
	bin/gobuild.sh $@ istio.io/istio/pkg/version ./pilot/cmd/$(@F)

# Non-static istioctls. These are typically a build artifact so placed in out/ rather than bin/ .
${ISTIO_OUT}/istioctl-linux: depend
	STATIC=0 GOOS=linux   bin/gobuild.sh $@ istio.io/istio/pkg/version ./pilot/cmd/istioctl
${ISTIO_OUT}/istioctl-osx: depend
	STATIC=0 GOOS=darwin  bin/gobuild.sh $@ istio.io/istio/pkg/version ./pilot/cmd/istioctl
${ISTIO_OUT}/istioctl-win.exe: depend
	STATIC=0 GOOS=windows bin/gobuild.sh $@ istio.io/istio/pkg/version ./pilot/cmd/istioctl

MIXER_GO_BINS:=${ISTIO_OUT}/mixs ${ISTIO_OUT}/mixc
$(MIXER_GO_BINS): depend
	bin/gobuild.sh $@ istio.io/istio/pkg/version ./mixer/cmd/$(@F)

${ISTIO_OUT}/servicegraph: depend
	bin/gobuild.sh $@ istio.io/istio/pkg/version ./mixer/example/$(@F)

SECURITY_GO_BINS:=${ISTIO_OUT}/node_agent ${ISTIO_OUT}/istio_ca
$(SECURITY_GO_BINS): depend
	bin/gobuild.sh $@ istio.io/istio/pkg/version ./security/cmd/$(@F)

.PHONY: go-build
go-build: $(PILOT_GO_BINS) $(MIXER_GO_BINS) $(SECURITY_GO_BINS)

# The following are convenience aliases for most of the go targets
# The first block is for aliases that are the same as the actual binary,
# while the ones that follow need slight adjustments to their names.

IDENTITY_ALIAS_LIST:=istioctl mixc mixs pilot-agent servicegraph sidecar-injector
.PHONY: $(IDENTITY_ALIAS_LIST)
$(foreach ITEM,$(IDENTITY_ALIAS_LIST),$(eval $(ITEM): ${ISTIO_OUT}/$(ITEM)))

.PHONY: istio-ca
istio-ca: ${ISTIO_OUT}/istio_ca

.PHONY: node-agent
node-agent: ${ISTIO_OUT}/node_agent

.PHONY: pilot
pilot: ${ISTIO_OUT}/pilot-discovery

# istioctl-all makes all of the non-static istioctl executables for each supported OS
.PHONY: istioctl-all
istioctl-all: ${ISTIO_OUT}/istioctl-linux ${ISTIO_OUT}/istioctl-osx ${ISTIO_OUT}/istioctl-win.exe

.PHONY: istio-archive

istio-archive: ${ISTIO_OUT}/archive

# TBD: how to capture VERSION, ISTIO_DOCKER_HUB, ISTIO_URL, ISTIO_URL_ISTIOCTL as dependencies
${ISTIO_OUT}/archive: istioctl-all LICENSE README.md istio.VERSION install/updateVersion.sh release/create_release_archives.sh
	rm -rf ${ISTIO_OUT}/archive
	mkdir -p ${ISTIO_OUT}/archive/istioctl
	cp ${ISTIO_OUT}/istioctl-* ${ISTIO_OUT}/archive/istioctl/
	cp LICENSE ${ISTIO_OUT}/archive
	cp README.md ${ISTIO_OUT}/archive
	install/updateVersion.sh -c "$(ISTIO_DOCKER_HUB),$(VERSION)" -A "$(ISTIO_URL)/deb" \
                                 -x "$(ISTIO_DOCKER_HUB),$(VERSION)" -p "$(ISTIO_DOCKER_HUB),$(VERSION)" \
                                 -i "$(ISTIO_URL)/$(ISTIO_URL_ISTIOCTL)" \
                                 -P "$(ISTIO_URL)/deb" \
                                 -r "$(VERSION)" -E "$(ISTIO_URL)/deb" -d "${ISTIO_OUT}/archive"
	release/create_release_archives.sh -v "$(VERSION)" -o "${ISTIO_OUT}/archive"

#-----------------------------------------------------------------------------
# Target: go test
#-----------------------------------------------------------------------------

.PHONY: go-test localTestEnv test-bins

GOTEST_PARALLEL ?= '-test.parallel=4'
GOTEST_P ?= -p 1
GOSTATIC = -ldflags '-extldflags "-static"'

PILOT_TEST_BINS:=${ISTIO_OUT}/pilot-test-server ${ISTIO_OUT}/pilot-test-client ${ISTIO_OUT}/pilot-test-eurekamirror

$(PILOT_TEST_BINS): depend
	CGO_ENABLED=0 go build ${GOSTATIC} -o $@ istio.io/istio/$(subst -,/,$(@F))

# circleci expects this to end up in the bin directory
test-bins: $(PILOT_TEST_BINS)
	go build -o ${ISTIO_OUT}/pilot-integration-test istio.io/istio/pilot/test/integration
	cp ${ISTIO_OUT}/pilot-integration-test ${ISTIO_BIN}/

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

.PHONY: galley-test
galley-test: depend
	go test ${T} ./galley/...

.PHONY: security-test
security-test:
	go test ${T} ./security/pkg/...
	go test ${T} ./security/cmd/...

common-test:
	go test ${T} ./pkg/...

# Run tests
go-test: pilot-test mixer-test security-test broker-test galley-test common-test

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

.PHONY: galley-coverage
galley-coverage:
	bin/parallel-codecov.sh galley

.PHONY: security-coverage
security-coverage:
	bin/parallel-codecov.sh security/pkg
	bin/parallel-codecov.sh security/cmd

# Run coverage tests
coverage: pilot-coverage mixer-coverage security-coverage broker-coverage galley-coverage

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

.PHONY: galley-racetest
galley-racetest: depend
	go test ${T} -race ./galley/...

.PHONY: security-racetest
security-racetest:
	go test ${T} -race ./security/...

common-racetest:
	go test ${T} -race ./pkg/...

# Run race tests
racetest: pilot-racetest mixer-racetest security-racetest broker-racetest galley-test common-racetest

#-----------------------------------------------------------------------------
# Target: precommit
#-----------------------------------------------------------------------------
.PHONY: clean
.PHONY: clean.go

DIRS_TO_CLEAN:=${ISTIO_OUT}
FILES_TO_CLEAN:=

clean: clean.go
	rm -rf $(DIRS_TO_CLEAN)
	rm -f $(FILES_TO_CLEAN)

clean.go: ; $(info $(H) cleaning...)
	$(eval GO_CLEAN_FLAGS := -i -r)
	$(Q) $(GO) clean $(GO_CLEAN_FLAGS)

test: setup go-test

##################################################################################

# for now docker is limited to Linux compiles
ifeq ($(GOOS),linux)

include tools/istio-docker.mk

endif # end of docker block that's restricted to Linux

push.istioctl-all: istioctl-all
	gsutil -m cp -r "${ISTIO_OUT}"/istioctl-* "gs://${GS_BUCKET}/pilot/${TAG}/artifacts/istioctl"

artifacts: docker
	@echo 'To be added'

pilot/pkg/kube/config:
	touch $@

kubelink:
	ln -fs ~/.kube/config pilot/pkg/kube/

installgen:
	install/updateVersion.sh -a ${HUB},${TAG}

# files genarated by the default invocation of updateVersion.sh
FILES_TO_CLEAN+=install/consul/istio.yaml \
                install/eureka/istio.yaml \
                install/kubernetes/helm/istio/values.yaml \
                install/kubernetes/istio-auth.yaml \
                install/kubernetes/istio-ca-plugin-certs.yaml \
                install/kubernetes/istio-initializer.yaml \
                install/kubernetes/istio-one-namespace-auth.yaml \
                install/kubernetes/istio-one-namespace.yaml \
                install/kubernetes/istio.yaml \
                samples/bookinfo/consul/bookinfo.sidecars.yaml \
                samples/bookinfo/eureka/bookinfo.sidecars.yaml

.PHONY: artifacts build clean docker test setup push kubelink installgen

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


${ISTIO_OUT}/dist/Gopkg.lock:
	mkdir -p ${ISTIO_OUT}/dist
	cp Gopkg.lock ${ISTIO_OUT}/dist/

# Binary/built artifacts of the distribution
dist-bin: ${ISTIO_OUT}/dist/Gopkg.lock

dist: dist-bin

include .circleci/Makefile

# Building the debian file, docker.istio.deb and istio.deb
include tools/deb/istio.mk

#-----------------------------------------------------------------------------
# Target: e2e tests
#-----------------------------------------------------------------------------
include tests/istio.mk

#-----------------------------------------------------------------------------
# Target: bench check
#-----------------------------------------------------------------------------

.PHONY: benchcheck
benchcheck:
	bin/perfcheck.sh
