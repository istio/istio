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
export ISTIO_GO
SHELL := /bin/bash

# Current version, updated after a release.
VERSION ?= 0.5.0

# locations where artifacts are stored
ISTIO_DOCKER_HUB ?= docker.io/istio
ISTIO_GCS ?= istio-release/releases/$(VERSION)
ISTIO_URL ?= https://storage.googleapis.com/$(ISTIO_GCS)
ISTIO_URL_ISTIOCTL ?= istioctl

# cumulatively track the directories/files to delete after a clean
DIRS_TO_CLEAN:=
FILES_TO_CLEAN:=

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

#-----------------------------------------------------------------------------
# Output control
#-----------------------------------------------------------------------------
# Invoke make VERBOSE=1 to enable echoing of the command being executed
VERBOSE ?= 0
# Place the variable Q in front of a command to control echoing of the command being executed.
Q = $(if $(filter 1,$VERBOSE),,@)
# Use the variable H to add a header (equivalent to =>) to informational output
H = $(shell printf "\033[34;1m=>\033[0m")

# To build Pilot, Mixer and CA with debugger information, use DEBUG=1 when invoking make
ifeq ($(origin DEBUG), undefined)
BUILDTYPE_DIR:=release
else ifeq ($(DEBUG),0)
BUILDTYPE_DIR:=release
else
BUILDTYPE_DIR:=debug
export GCFLAGS:=-N -l
$(info $(H) Build with debugger information)
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
# Using same package structure as pkg/
export ISTIO_OUT:=$(GO_TOP)/out/$(GOOS)_$(GOARCH)/$(BUILDTYPE_DIR)

# scratch dir: this shouldn't be simply 'docker' since that's used for docker.save to store tar.gz files
ISTIO_DOCKER:=${ISTIO_OUT}/docker_temp

# dir where tar.gz files from docker.save are stored
ISTIO_DOCKER_TAR:=${ISTIO_OUT}/docker

GO_VERSION_REQUIRED:=1.9

HUB?=istio
ifeq ($(HUB),)
  $(error "HUB cannot be empty")
endif

# If tag not explicitly set in users' .istiorc.mk or command line, default to the git sha.
TAG ?= $(shell git rev-parse --verify HEAD)
ifeq ($(TAG),)
  $(error "TAG cannot be empty")
endif

# Discover if user has dep installed -- prefer that
DEP    := $(shell which dep    || echo "${ISTIO_BIN}/dep" )
GOLINT := $(shell which golint || echo "${ISTIO_BIN}/golint" )

# Set Google Storage bucket if not set
GS_BUCKET ?= istio-artifacts

.PHONY: default
default: depend build test

# The point of these is to allow scripts to query where artifacts
# are stored so that tests and other consumers of the build don't
# need to be updated to follow the changes in the Makefiles.
# Note that the query needs to pass the same types of parameters
# (e.g., DEBUG=0, GOOS=linux) as the actual build for the query
# to provide an accurate result.
.PHONY: where-is-out where-is-docker-temp where-is-docker-tar
where-is-out:
	@echo ${ISTIO_OUT}
where-is-docker-temp:
	@echo ${ISTIO_DOCKER}
where-is-docker-tar:
	@echo ${ISTIO_DOCKER_TAR}

#-----------------------------------------------------------------------------
# Target: depend
#-----------------------------------------------------------------------------
.PHONY: depend depend.ensure depend.status depend.update depend.view init

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

# Downloads envoy, based on the SHA defined in the base pilot Dockerfile
# Will also check vendor, based on Gopkg.lock
init: check-go-version $(ISTIO_OUT)/istio_is_init

# I tried to make this dependent on what I thought was the appropriate
# lock file, but it caused the rule for that file to get run (which
# seems to be about obtaining a new version of the 3rd party libraries).
$(ISTIO_OUT)/istio_is_init: bin/init.sh pilot/docker/Dockerfile.proxy_debug | ${ISTIO_OUT} ${DEP}
	@(DEP=${DEP} ISTIO_OUT=${ISTIO_OUT} bin/init.sh)
	@touch $(ISTIO_OUT)/istio_is_init

# init.sh downloads envoy
${ISTIO_OUT}/envoy: init

# Pull depdendencies, based on the checked in Gopkg.lock file.
# Developers must manually call dep.update if adding new deps or to pull recent
# changes.
depend: init | $(ISTIO_OUT)

$(ISTIO_OUT) $(ISTIO_BIN):
	@mkdir -p $@

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

#-----------------------------------------------------------------------------
# Target: precommit
#-----------------------------------------------------------------------------
.PHONY: precommit format format.gofmt format.goimports lint buildcache

# Target run by the pre-commit script, to automate formatting and lint
# If pre-commit script is not used, please run this manually.
precommit: format lint

format: format.gofmt format.goimports

format.gofmt: ; $(info $(H) formatting files with go fmt...)
	$(Q) gofmt -s -w $$($(GO_FILES_CMD))

format.goimports: ; $(info $(H) formatting files with goimports...)
	$(Q) goimports -w -local istio.io $$($(GO_FILES_CMD))

# Build with -i to store the build caches into $GOPATH/pkg
buildcache:
	GOBUILDFLAGS=-i $(MAKE) build

# Existence of build cache .a files actually affects the results of
# some linters; they need to exist.
lint: buildcache
	SKIP_INIT=1 bin/linters.sh

# @todo gometalinter targets?

#-----------------------------------------------------------------------------
# Target: go build
#-----------------------------------------------------------------------------

# gobuild script uses custom linker flag to set the variables.
# Params: OUT VERSION_PKG SRC

PILOT_GO_BINS:=${ISTIO_OUT}/pilot-discovery ${ISTIO_OUT}/pilot-agent \
               ${ISTIO_OUT}/istioctl ${ISTIO_OUT}/sidecar-injector
$(PILOT_GO_BINS): depend
	bin/gobuild.sh $@ istio.io/istio/pkg/version ./pilot/cmd/$(@F)

# Non-static istioctls. These are typically a build artifact.
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

SECURITY_GO_BINS:=${ISTIO_OUT}/node_agent ${ISTIO_OUT}/istio_ca ${ISTIO_OUT}/multicluster_ca
$(SECURITY_GO_BINS): depend
	bin/gobuild.sh $@ istio.io/istio/pkg/version ./security/cmd/$(@F)

.PHONY: build
build: $(PILOT_GO_BINS) $(MIXER_GO_BINS) $(SECURITY_GO_BINS)

# The following are convenience aliases for most of the go targets
# The first block is for aliases that are the same as the actual binary,
# while the ones that follow need slight adjustments to their names.

IDENTITY_ALIAS_LIST:=istioctl mixc mixs pilot-agent servicegraph sidecar-injector multicluster_ca
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
# consider using -a with updateVersion.sh to simplify the input parameters
${ISTIO_OUT}/archive: istioctl-all LICENSE README.md istio.VERSION install/updateVersion.sh release/create_release_archives.sh
	rm -rf ${ISTIO_OUT}/archive
	mkdir -p ${ISTIO_OUT}/archive/istioctl
	cp ${ISTIO_OUT}/istioctl-* ${ISTIO_OUT}/archive/istioctl/
	cp LICENSE ${ISTIO_OUT}/archive
	cp README.md ${ISTIO_OUT}/archive
	cp -r tools ${ISTIO_OUT}/archive
	install/updateVersion.sh -c "$(ISTIO_DOCKER_HUB),$(VERSION)" \
                                 -x "$(ISTIO_DOCKER_HUB),$(VERSION)" -p "$(ISTIO_DOCKER_HUB),$(VERSION)" \
                                 -i "$(ISTIO_URL)/$(ISTIO_URL_ISTIOCTL)" \
                                 -P "$(ISTIO_URL)/deb" \
                                 -r "$(VERSION)" -d "${ISTIO_OUT}/archive"
	release/create_release_archives.sh -v "$(VERSION)" -o "${ISTIO_OUT}/archive"

#-----------------------------------------------------------------------------
# Target: test
#-----------------------------------------------------------------------------

.PHONY: test localTestEnv test-bins

# Run coverage tests
test: pilot-test mixer-test security-test broker-test galley-test common-test

GOTEST_PARALLEL ?= '-test.parallel=4'
GOTEST_P ?= -p 1
GOSTATIC = -ldflags '-extldflags "-static"'

PILOT_TEST_BINS:=${ISTIO_OUT}/pilot-test-server ${ISTIO_OUT}/pilot-test-client ${ISTIO_OUT}/pilot-test-eurekamirror

$(PILOT_TEST_BINS): depend
	CGO_ENABLED=0 go build ${GOSTATIC} -o $@ istio.io/istio/$(subst -,/,$(@F))

test-bins: $(PILOT_TEST_BINS)
	go build -o ${ISTIO_OUT}/pilot-integration-test istio.io/istio/pilot/test/integration

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

.PHONY: common-test
common-test:
	go test ${T} ./pkg/...

#-----------------------------------------------------------------------------
# Target: coverage
#-----------------------------------------------------------------------------

.PHONY: coverage

# Run coverage tests
coverage: pilot-coverage mixer-coverage security-coverage broker-coverage galley-coverage

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

#-----------------------------------------------------------------------------
# Target: go test -race
#-----------------------------------------------------------------------------

.PHONY: racetest

# Run race tests
racetest: pilot-racetest mixer-racetest security-racetest broker-racetest galley-test common-racetest

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

.PHONY: common-racetest
common-racetest:
	go test ${T} -race ./pkg/...

#-----------------------------------------------------------------------------
# Target: clean
#-----------------------------------------------------------------------------
.PHONY: clean clean.go

DIRS_TO_CLEAN+=${ISTIO_OUT}

clean: clean.go
	rm -rf $(DIRS_TO_CLEAN)
	rm -f $(FILES_TO_CLEAN)

clean.go: ; $(info $(H) cleaning...)
	$(eval GO_CLEAN_FLAGS := -i -r)
	$(Q) $(GO) clean $(GO_CLEAN_FLAGS)

#-----------------------------------------------------------------------------
# Target: docker
#-----------------------------------------------------------------------------
.PHONY: artifacts gcs.push.istioctl-all artifacts installgen

# for now docker is limited to Linux compiles
ifeq ($(GOOS),linux)

include tools/istio-docker.mk

endif # end of docker block that's restricted to Linux

gcs.push.istioctl-all: istioctl-all
	gsutil -m cp -r "${ISTIO_OUT}"/istioctl-* "gs://${GS_BUCKET}/pilot/${TAG}/artifacts/istioctl"

artifacts: docker
	@echo 'To be added'

# generate_yaml in tests/istio.mk can build without specifying a hub & tag
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
.PHONY: dist dist-bin

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
