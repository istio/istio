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
SHELL := /bin/bash -o pipefail

VERSION ?= 1.5-dev

# Base version of Istio image to use
BASE_VERSION ?= 1.5-dev.0

export GO111MODULE ?= on
export GOPROXY ?= https://proxy.golang.org
export GOSUMDB ?= sum.golang.org

# locations where artifacts are stored
ISTIO_DOCKER_HUB ?= docker.io/istio
export ISTIO_DOCKER_HUB
ISTIO_GCS ?= istio-release/releases/$(VERSION)
ISTIO_URL ?= https://storage.googleapis.com/$(ISTIO_GCS)
ISTIO_CNI_HUB ?= gcr.io/istio-testing
export ISTIO_CNI_HUB
ISTIO_CNI_TAG ?= latest
export ISTIO_CNI_TAG

# cumulatively track the directories/files to delete after a clean
DIRS_TO_CLEAN:=
FILES_TO_CLEAN:=

# If GOPATH is not set by the env, set it to a sane value
GOPATH ?= $(shell cd ${ISTIO_GO}/../../..; pwd)
export GOPATH

# If GOPATH is made up of several paths, use the first one for our targets in this Makefile
GO_TOP := $(shell echo ${GOPATH} | cut -d ':' -f1)
export GO_TOP

# Note that disabling cgo here adversely affects go get.  Instead we'll rely on this
# to be handled in common/scripts/gobuild.sh
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

LOCAL_ARCH := $(shell uname -m)
ifeq ($(LOCAL_ARCH),x86_64)
GOARCH_LOCAL := amd64
else ifeq ($(shell echo $(LOCAL_ARCH) | head -c 5),armv8)
GOARCH_LOCAL := arm64
else ifeq ($(shell echo $(LOCAL_ARCH) | head -c 4),armv)
GOARCH_LOCAL := arm
else
GOARCH_LOCAL := $(LOCAL_ARCH)
endif
export GOARCH ?= $(GOARCH_LOCAL)

LOCAL_OS := $(shell uname)
ifeq ($(LOCAL_OS),Linux)
   export GOOS_LOCAL = linux
else ifeq ($(LOCAL_OS),Darwin)
   export GOOS_LOCAL = darwin
else
   $(error "This system's OS $(LOCAL_OS) isn't recognized/supported")
   # export GOOS_LOCAL ?= windows
endif

export GOOS ?= $(GOOS_LOCAL)

export ENABLE_COREDUMP ?= false

# Enable Istio CNI in helm template commands
export ENABLE_ISTIO_CNI ?= false

# NOTE: env var EXTRA_HELM_SETTINGS can contain helm chart override settings, example:
# EXTRA_HELM_SETTINGS="--set istio-cni.excludeNamespaces={} --set-string istio-cni.tag=v0.1-dev-foo"

#-----------------------------------------------------------------------------
# Output control
#-----------------------------------------------------------------------------
# Invoke make VERBOSE=1 to enable echoing of the command being executed
export VERBOSE ?= 0
# Place the variable Q in front of a command to control echoing of the command being executed.
Q = $(if $(filter 1,$VERBOSE),,@)
# Use the variable H to add a header (equivalent to =>) to informational output
H = $(shell printf "\033[34;1m=>\033[0m")

# To build Pilot, Mixer and CA with debugger information, use DEBUG=1 when invoking make
goVerStr := $(shell $(GO) version | awk '{split($$0,a," ")}; {print a[3]}')
goVerNum := $(shell echo $(goVerStr) | awk '{split($$0,a,"go")}; {print a[2]}')
goVerMajor := $(shell echo $(goVerNum) | awk '{split($$0, a, ".")}; {print a[1]}')
goVerMinor := $(shell echo $(goVerNum) | awk '{split($$0, a, ".")}; {print a[2]}' | sed -e 's/\([0-9]\+\).*/\1/')
gcflagsPattern := $(shell ( [ $(goVerMajor) -ge 1 ] && [ ${goVerMinor} -ge 10 ] ) && echo 'all=' || echo '')

ifeq ($(origin DEBUG), undefined)
  BUILDTYPE_DIR:=release
else ifeq ($(DEBUG),0)
  BUILDTYPE_DIR:=release
else
  BUILDTYPE_DIR:=debug
  export GCFLAGS:=$(gcflagsPattern)-N -l
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
export OUT_DIR=$(GO_TOP)/out
export ISTIO_OUT:=$(OUT_DIR)/$(GOOS)_$(GOARCH)/$(BUILDTYPE_DIR)
export ISTIO_OUT_LINUX:=$(OUT_DIR)/linux_amd64/$(BUILDTYPE_DIR)
export HELM=$(ISTIO_OUT)/helm
export ARTIFACTS ?= $(ISTIO_OUT)
export REPO_ROOT := $(shell git rev-parse --show-toplevel)

# scratch dir: this shouldn't be simply 'docker' since that's used for docker.save to store tar.gz files
ISTIO_DOCKER:=${ISTIO_OUT_LINUX}/docker_temp
# Config file used for building istio:proxy container.
DOCKER_PROXY_CFG?=Dockerfile.proxy

# scratch dir for building isolated images. Please don't remove it again - using
# ISTIO_DOCKER results in slowdown, all files (including multiple copies of envoy) will be
# copied to the docker temp container - even if you add only a tiny file, >1G of data will
# be copied, for each docker image.
DOCKER_BUILD_TOP:=${ISTIO_OUT_LINUX}/docker_build

# dir where tar.gz files from docker.save are stored
ISTIO_DOCKER_TAR:=${ISTIO_OUT_LINUX}/docker

# Populate the git version for istio/proxy (i.e. Envoy)
ifeq ($(PROXY_REPO_SHA),)
  export PROXY_REPO_SHA:=$(shell grep PROXY_REPO_SHA istio.deps  -A 4 | grep lastStableSHA | cut -f 4 -d '"')
endif

# Envoy binary variables Keep the default URLs up-to-date with the latest push from istio/proxy.

# OS-neutral vars. These currently only work for linux.
export ISTIO_ENVOY_VERSION ?= ${PROXY_REPO_SHA}
export ISTIO_ENVOY_DEBUG_URL ?= https://storage.googleapis.com/istio-build/proxy/envoy-debug-$(ISTIO_ENVOY_VERSION).tar.gz
export ISTIO_ENVOY_RELEASE_URL ?= https://storage.googleapis.com/istio-build/proxy/envoy-alpha-$(ISTIO_ENVOY_VERSION).tar.gz

# Envoy Linux vars.
export ISTIO_ENVOY_LINUX_VERSION ?= ${ISTIO_ENVOY_VERSION}
export ISTIO_ENVOY_LINUX_DEBUG_URL ?= ${ISTIO_ENVOY_DEBUG_URL}
export ISTIO_ENVOY_LINUX_RELEASE_URL ?= ${ISTIO_ENVOY_RELEASE_URL}
# Variables for the extracted debug/release Envoy artifacts.
export ISTIO_ENVOY_LINUX_DEBUG_DIR ?= ${OUT_DIR}/linux_amd64/debug
export ISTIO_ENVOY_LINUX_DEBUG_NAME ?= envoy-debug-${ISTIO_ENVOY_LINUX_VERSION}
export ISTIO_ENVOY_LINUX_DEBUG_PATH ?= ${ISTIO_ENVOY_LINUX_DEBUG_DIR}/${ISTIO_ENVOY_LINUX_DEBUG_NAME}
export ISTIO_ENVOY_LINUX_RELEASE_DIR ?= ${OUT_DIR}/linux_amd64/release
export ISTIO_ENVOY_LINUX_RELEASE_NAME ?= envoy-${ISTIO_ENVOY_VERSION}
export ISTIO_ENVOY_LINUX_RELEASE_PATH ?= ${ISTIO_ENVOY_LINUX_RELEASE_DIR}/${ISTIO_ENVOY_LINUX_RELEASE_NAME}

# Envoy macOS vars.
# TODO Change url when official envoy release for macOS is available
export ISTIO_ENVOY_MACOS_VERSION ?= 1.0.2
export ISTIO_ENVOY_MACOS_RELEASE_URL ?= https://github.com/istio/proxy/releases/download/${ISTIO_ENVOY_MACOS_VERSION}/istio-proxy-${ISTIO_ENVOY_MACOS_VERSION}-macos.tar.gz
# Variables for the extracted debug/release Envoy artifacts.
export ISTIO_ENVOY_MACOS_RELEASE_DIR ?= ${OUT_DIR}/darwin_amd64/release
export ISTIO_ENVOY_MACOS_RELEASE_NAME ?= envoy-${ISTIO_ENVOY_MACOS_VERSION}
export ISTIO_ENVOY_MACOS_RELEASE_PATH ?= ${ISTIO_ENVOY_MACOS_RELEASE_DIR}/${ISTIO_ENVOY_MACOS_RELEASE_NAME}

# Allow user-override for a local Envoy build.
export USE_LOCAL_PROXY ?= 0
ifeq ($(USE_LOCAL_PROXY),1)
  export ISTIO_ENVOY_LOCAL ?= $(realpath ${ISTIO_GO}/../proxy/bazel-bin/src/envoy/envoy)
  # Point the native paths to the local envoy build.
  ifeq ($(GOOS_LOCAL), Darwin)
    export ISTIO_ENVOY_MACOS_RELEASE_DIR = $(dir ${ISTIO_ENVOY_LOCAL})
    export ISTIO_ENVOY_MACOS_RELEASE_PATH = ${ISTIO_ENVOY_LOCAL}
  else
    export ISTIO_ENVOY_LINUX_DEBUG_DIR = $(dir ${ISTIO_ENVOY_LOCAL})
    export ISTIO_ENVOY_LINUX_RELEASE_DIR = $(dir ${ISTIO_ENVOY_LOCAL})
    export ISTIO_ENVOY_LINUX_DEBUG_PATH = ${ISTIO_ENVOY_LOCAL}
    export ISTIO_ENVOY_LINUX_RELEASE_PATH = ${ISTIO_ENVOY_LOCAL}
  endif
endif

GO_VERSION_REQUIRED:=1.10

HUB?=istio
ifeq ($(HUB),)
  $(error "HUB cannot be empty")
endif

# If tag not explicitly set in users' .istiorc.mk or command line, default to the git sha.
TAG ?= $(shell git rev-parse --verify HEAD)
ifeq ($(TAG),)
  $(error "TAG cannot be empty")
endif

VARIANT :=
ifeq ($(VARIANT),)
  TAG_VARIANT:=${TAG}
else
  TAG_VARIANT:=${TAG}-${VARIANT}
endif

PULL_POLICY ?= IfNotPresent
ifeq ($(TAG),latest)
  PULL_POLICY = Always
endif
ifeq ($(PULL_POLICY),)
  $(error "PULL_POLICY cannot be empty")
endif

GEN_CERT := ${ISTIO_BIN}/generate_cert

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
.PHONY: depend init

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
init: check-go-version $(ISTIO_OUT)/istio_is_init
	mkdir -p ${OUT_DIR}/logs

# Sync is the same as init in release branch. In master this pulls from master.
sync: init

# I tried to make this dependent on what I thought was the appropriate
# lock file, but it caused the rule for that file to get run (which
# seems to be about obtaining a new version of the 3rd party libraries).
$(ISTIO_OUT)/istio_is_init: bin/init.sh istio.deps | $(ISTIO_OUT)
	ISTIO_OUT=$(ISTIO_OUT) bin/init.sh
	touch $(ISTIO_OUT)/istio_is_init

# init.sh downloads envoy
${ISTIO_OUT}/envoy: init
${ISTIO_ENVOY_LINUX_DEBUG_PATH}: init
${ISTIO_ENVOY_LINUX_RELEASE_PATH}: init
${ISTIO_ENVOY_MACOS_RELEASE_PATH}: init

# Pull dependencies, based on the checked in Gopkg.lock file.
# Developers must manually run `dep ensure` if adding new deps
depend: init | $(ISTIO_OUT)

OUTPUT_DIRS = $(ISTIO_OUT) $(ISTIO_BIN)
DIRS_TO_CLEAN+=${ISTIO_OUT}
ifneq ($(ISTIO_OUT),$(ISTIO_OUT_LINUX))
  OUTPUT_DIRS += $(ISTIO_OUT_LINUX)
  DIRS_TO_CLEAN += $(ISTIO_OUT_LINUX)
endif

$(OUTPUT_DIRS):
	@mkdir -p $@

.PHONY: ${GEN_CERT}
${GEN_CERT}:
	GOOS=$(GOOS_LOCAL) && GOARCH=$(GOARCH_LOCAL) && CGO_ENABLED=1 common/scripts/gobuild.sh $@ ./security/tools/generate_cert

#-----------------------------------------------------------------------------
# Target: precommit
#-----------------------------------------------------------------------------
.PHONY: precommit format format.gofmt format.goimports lint buildcache

# Target run by the pre-commit script, to automate formatting and lint
# If pre-commit script is not used, please run this manually.
precommit: format lint

format: fmt

fmt: format-go format-python
	go mod tidy

# Build with -i to store the build caches into $GOPATH/pkg
buildcache:
	GOBUILDFLAGS=-i $(MAKE) -f Makefile.core.mk build

# List of all binaries to build
BINARIES:=./istioctl/cmd/istioctl \
  ./pilot/cmd/pilot-discovery \
  ./pilot/cmd/pilot-agent \
  ./sidecar-injector/cmd/sidecar-injector \
  ./mixer/cmd/mixs \
  ./mixer/cmd/mixc \
  ./mixer/tools/mixgen \
  ./galley/cmd/galley \
  ./security/cmd/node_agent \
  ./security/cmd/node_agent_k8s \
  ./security/cmd/istio_ca \
  ./security/tools/sdsclient \
  ./pkg/test/echo/cmd/client \
  ./pkg/test/echo/cmd/server \
  ./mixer/test/policybackend \
  ./cmd/istiod \
  ./tools/hyperistio \
  ./tools/istio-iptables \
  ./tools/istio-clean-iptables

# List of binaries included in releases
RELEASE_BINARIES:=pilot-discovery pilot-agent sidecar-injector mixc mixs mixgen node_agent node_agent_k8s istio_ca istiod istioctl galley sdsclient

.PHONY: build
build: depend
	STATIC=0 GOOS=$(GOOS) GOARCH=$(GOARCH) LDFLAGS='-extldflags -static -s -w' common/scripts/gobuild.sh $(ISTIO_OUT)/ $(BINARIES)

.PHONY: build-linux
build-linux: depend
	STATIC=0 GOOS=linux GOARCH=amd64 LDFLAGS='-extldflags -static -s -w' common/scripts/gobuild.sh $(ISTIO_OUT_LINUX)/ $(BINARIES)

# Create targets for ISTIO_OUT_LINUX/binary
$(foreach bin,$(BINARIES),$(ISTIO_OUT_LINUX)/$(shell basename $(bin))): build-linux

# Create helper targets for each binary, like "pilot-discovery"
# As an optimization, these still build everything
$(foreach bin,$(BINARIES),$(shell basename $(bin))): build

MARKDOWN_LINT_WHITELIST=localhost:8080,storage.googleapis.com/istio-artifacts/pilot/,http://ratings.default.svc.cluster.local:9080/ratings

lint: lint-python lint-copyright-banner lint-scripts lint-dockerfiles lint-markdown lint-yaml lint-licenses
	@bin/check_helm.sh
	@bin/check_samples.sh
	@bin/check_dashboards.sh
	@go run mixer/tools/adapterlinter/main.go ./mixer/adapter/...
	@golangci-lint run -c ./common/config/.golangci.yml ./galley/...
	@golangci-lint run -c ./common/config/.golangci.yml ./istioctl/...
	@golangci-lint run -c ./common/config/.golangci.yml ./mixer/...
	@golangci-lint run -c ./common/config/.golangci.yml ./pilot/...
	@golangci-lint run -c ./common/config/.golangci.yml ./pkg/...
	@golangci-lint run -c ./common/config/.golangci.yml ./samples/...
	@golangci-lint run -c ./common/config/.golangci.yml ./security/...
	@golangci-lint run -c ./common/config/.golangci.yml ./sidecar-injector/...
	@golangci-lint run -c ./common/config/.golangci.yml ./tests/...
	@golangci-lint run -c ./common/config/.golangci.yml ./tools/...
	@testlinter
	@envvarlinter galley istioctl mixer pilot security sidecar-injector

gen:
	@mkdir -p /tmp/bin
	@go build -o /tmp/bin/mixgen "${REPO_ROOT}/mixer/tools/mixgen/main.go"
	@PATH=${PATH}:/tmp/bin go generate ./...

gen-check: gen check-clean-repo

#-----------------------------------------------------------------------------
# Target: go build
#-----------------------------------------------------------------------------

# gobuild script uses custom linker flag to set the variables.
# Params: OUT VERSION_PKG SRC

RELEASE_LDFLAGS='-extldflags -static -s -w'
DEBUG_LDFLAGS='-extldflags "-static"'

# Non-static istioctl targets. These are typically a build artifact.
${ISTIO_OUT}/istioctl-linux: depend
	STATIC=0 GOOS=linux LDFLAGS=$(RELEASE_LDFLAGS) common/scripts/gobuild.sh $@ ./istioctl/cmd/istioctl
${ISTIO_OUT}/istioctl-osx: depend
	STATIC=0 GOOS=darwin LDFLAGS=$(RELEASE_LDFLAGS) common/scripts/gobuild.sh $@ ./istioctl/cmd/istioctl
${ISTIO_OUT}/istioctl-win.exe: depend
	STATIC=0 GOOS=windows LDFLAGS=$(RELEASE_LDFLAGS) common/scripts/gobuild.sh $@ ./istioctl/cmd/istioctl

# generate the istioctl completion files
${ISTIO_OUT}/istioctl.bash: istioctl
	${ISTIO_OUT}/istioctl collateral --bash && \
	mv istioctl.bash ${ISTIO_OUT}/istioctl.bash

${ISTIO_OUT}/_istioctl: istioctl
	${ISTIO_OUT}/istioctl collateral --zsh && \
	mv _istioctl ${ISTIO_OUT}/_istioctl

.PHONY: binaries-test
binaries-test:
	go test ./tests/binary/... -v --base-dir ${ISTIO_OUT} --binaries="$(RELEASE_BINARIES)"

# istioctl-all makes all of the non-static istioctl executables for each supported OS
.PHONY: istioctl-all
istioctl-all: ${ISTIO_OUT}/istioctl-linux ${ISTIO_OUT}/istioctl-osx ${ISTIO_OUT}/istioctl-win.exe

.PHONY: istioctl.completion
istioctl.completion: ${ISTIO_OUT}/istioctl.bash ${ISTIO_OUT}/_istioctl

# istioctl-install builds then installs istioctl into $GOPATH/BIN
# Used for debugging istioctl during dev work
.PHONY: istioctl-install
istioctl-install:
	go install istio.io/istio/istioctl/cmd/istioctl

#-----------------------------------------------------------------------------
# Target: test
#-----------------------------------------------------------------------------

.PHONY: test localTestEnv

JUNIT_REPORT := $(shell which go-junit-report 2> /dev/null || echo "${ISTIO_BIN}/go-junit-report")

${ISTIO_BIN}/go-junit-report:
	@echo "go-junit-report not found. Installing it now..."
	unset GOOS && unset GOARCH && CGO_ENABLED=1 go get -u github.com/jstemmer/go-junit-report

# Run coverage tests
JUNIT_UNIT_TEST_XML ?= $(ARTIFACTS)/junit_unit-tests.xml
ifeq ($(WHAT),)
       TEST_OBJ = common-test pilot-test mixer-test security-test galley-test istioctl-test
else
       TEST_OBJ = selected-pkg-test
endif
test: | $(JUNIT_REPORT)
	mkdir -p $(dir $(JUNIT_UNIT_TEST_XML))
	KUBECONFIG="$${KUBECONFIG:-$${REPO_ROOT}/tests/util/kubeconfig}" \
	$(MAKE) -f Makefile.core.mk --keep-going $(TEST_OBJ) \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_UNIT_TEST_XML))

GOTEST_PARALLEL ?= '-test.parallel=1'

localTestEnv: build
	bin/testEnvLocalK8S.sh ensure

localTestEnvCleanup: build
	bin/testEnvLocalK8S.sh stop
		
.PHONY: pilot-test
pilot-test:
	go test ${T} ./pilot/...

.PHONY: istioctl-test
istioctl-test:
	go test ${T} ./istioctl/...

.PHONY: mixer-test
MIXER_TEST_T ?= ${T} ${GOTEST_PARALLEL}
mixer-test:
	# Some tests use relative path "testdata", must be run from mixer dir
	(cd mixer; go test ${MIXER_TEST_T} ./...)

.PHONY: galley-test
galley-test:
	go test ${T} ./galley/...

.PHONY: security-test
security-test:
	go test ${T} ./security/pkg/...
	go test ${T} ./security/cmd/...

.PHONY: common-test
common-test: build
	go test ${T} ./pkg/...
	go test ${T} ./tests/common/...
	# Execute bash shell unit tests scripts
	./tests/scripts/scripts_test.sh
	./tests/scripts/istio-iptables-test.sh

.PHONY: selected-pkg-test
selected-pkg-test:
	find ${WHAT} -name "*_test.go" | xargs -I {} dirname {} | uniq | xargs -I {} go test ${T} ./{}

#-----------------------------------------------------------------------------
# Target: coverage
#-----------------------------------------------------------------------------

.PHONY: coverage

# Run coverage tests
coverage: pilot-coverage mixer-coverage security-coverage galley-coverage common-coverage istioctl-coverage

coverage-diff:
	./bin/codecov_diff.sh

.PHONY: pilot-coverage
pilot-coverage:
	bin/codecov.sh pilot

.PHONY: istioctl-coverage
istioctl-coverage:
	bin/codecov.sh istioctl

.PHONY: mixer-coverage
mixer-coverage:
	bin/codecov.sh mixer

.PHONY: galley-coverage
galley-coverage:
	bin/codecov.sh galley

.PHONY: security-coverage
security-coverage:
	bin/codecov.sh security/pkg
	bin/codecov.sh security/cmd

.PHONY: common-coverage
common-coverage:
	bin/codecov.sh pkg

#-----------------------------------------------------------------------------
# Target: go test -race
#-----------------------------------------------------------------------------

.PHONY: racetest

RACE_TESTS ?= pilot-racetest mixer-racetest security-racetest galley-test common-racetest istioctl-racetest
racetest: $(JUNIT_REPORT)
	mkdir -p $(dir $(JUNIT_UNIT_TEST_XML))
	$(MAKE) -f Makefile.core.mk --keep-going $(RACE_TESTS) \
	2>&1 | tee >($(JUNIT_REPORT) > $(JUNIT_UNIT_TEST_XML))

.PHONY: pilot-racetest
pilot-racetest:
	RACE_TEST=true go test ${T} -race ./pilot/...

.PHONY: istioctl-racetest
istioctl-racetest:
	RACE_TEST=true go test ${T} -race ./istioctl/...

.PHONY: mixer-racetest
mixer-racetest:
	# Some tests use relative path "testdata", must be run from mixer dir
	(cd mixer; RACE_TEST=true go test ${T} -race ./...)

.PHONY: galley-racetest
galley-racetest:
	RACE_TEST=true go test ${T} -race ./galley/...

.PHONY: security-racetest
security-racetest:
	RACE_TEST=true go test ${T} -race ./security/pkg/... ./security/cmd/...

.PHONY: common-racetest
common-racetest:
	RACE_TEST=true go test ${T} -race ./pkg/...

#-----------------------------------------------------------------------------
# Target: clean
#-----------------------------------------------------------------------------
.PHONY: clean clean.go

clean: clean.go
	rm -rf $(DIRS_TO_CLEAN)
	rm -f $(FILES_TO_CLEAN)

clean.go: ; $(info $(H) cleaning...)
	$(eval GO_CLEAN_FLAGS := -i -r)
	$(Q) $(GO) clean $(GO_CLEAN_FLAGS)

#-----------------------------------------------------------------------------
# Target: docker
#-----------------------------------------------------------------------------
.PHONY: push artifacts

# for now docker is limited to Linux compiles - why ?
include tools/istio-docker.mk

push: docker.push

$(HELM): $(ISTIO_OUT)
	bin/init_helm.sh

$(HOME)/.helm:
	$(HELM) init --client-only

# create istio-init.yaml
istio-init.yaml: $(HELM) $(HOME)/.helm
	cat install/kubernetes/namespace.yaml > install/kubernetes/$@
	cat install/kubernetes/helm/istio-init/files/crd-* >> install/kubernetes/$@
	$(HELM) template --name=istio --namespace=istio-system \
		--set-string global.tag=${TAG_VARIANT} \
		--set-string global.hub=${HUB} \
		install/kubernetes/helm/istio-init >> install/kubernetes/$@

# creates istio-demo.yaml istio-remote.yaml
# Ensure that values-$filename is present in install/kubernetes/helm/istio
istio-demo.yaml istio-remote.yaml istio-minimal.yaml: $(HELM) $(HOME)/.helm
	cat install/kubernetes/namespace.yaml > install/kubernetes/$@
	cat install/kubernetes/helm/istio-init/files/crd-* >> install/kubernetes/$@
	$(HELM) template \
		--name=istio \
		--namespace=istio-system \
		--set-string global.tag=${TAG_VARIANT} \
		--set-string global.hub=${HUB} \
		--set-string global.imagePullPolicy=$(PULL_POLICY) \
		--set global.proxy.enableCoreDump=${ENABLE_COREDUMP} \
		--set istio_cni.enabled=${ENABLE_ISTIO_CNI} \
		${EXTRA_HELM_SETTINGS} \
		--values install/kubernetes/helm/istio/values-$@ \
		install/kubernetes/helm/istio >> install/kubernetes/$@

e2e_files = istio-auth-non-mcp.yaml \
			istio-auth-sds.yaml \
			istio-non-mcp.yaml \
			istio.yaml \
			istio-auth.yaml \
			istio-auth-mcp.yaml \
			istio-auth-multicluster.yaml \
			istio-mcp.yaml \
			istio-one-namespace.yaml \
			istio-one-namespace-auth.yaml \
			istio-one-namespace-trust-domain.yaml \
			istio-multicluster.yaml \
			istio-multicluster-split-horizon.yaml \

FILES_TO_CLEAN+=install/consul/istio.yaml \
                install/kubernetes/istio-auth.yaml \
                install/kubernetes/istio-citadel-plugin-certs.yaml \
                install/kubernetes/istio-citadel-with-health-check.yaml \
                install/kubernetes/istio-one-namespace-auth.yaml \
                install/kubernetes/istio-one-namespace-trust-domain.yaml \
                install/kubernetes/istio-one-namespace.yaml \
                install/kubernetes/istio.yaml \
                samples/bookinfo/platform/consul/bookinfo.sidecars.yaml \

.PHONY: generate_e2e_yaml generate_e2e_yaml_coredump
generate_e2e_yaml: $(e2e_files)

generate_e2e_yaml_coredump: export ENABLE_COREDUMP=true
generate_e2e_yaml_coredump:
	$(MAKE) -f Makefile.core.mk generate_e2e_yaml

# Create yaml files for e2e tests. Applies values-e2e.yaml, then values-$filename.yaml
$(e2e_files): $(HELM) $(HOME)/.helm istio-init.yaml
	cat install/kubernetes/namespace.yaml > install/kubernetes/$@
	cat install/kubernetes/helm/istio-init/files/crd-* >> install/kubernetes/$@
	$(HELM) template \
		--name=istio \
		--namespace=istio-system \
		--set-string global.tag=${TAG_VARIANT} \
		--set-string global.hub=${HUB} \
		--set-string global.imagePullPolicy=$(PULL_POLICY) \
		--set global.proxy.enableCoreDump=${ENABLE_COREDUMP} \
		--set istio_cni.enabled=${ENABLE_ISTIO_CNI} \
		${EXTRA_HELM_SETTINGS} \
		--values install/kubernetes/helm/istio/test-values/values-e2e.yaml \
		--values install/kubernetes/helm/istio/test-values/values-$@ \
		install/kubernetes/helm/istio >> install/kubernetes/$@

#-----------------------------------------------------------------------------
# Target: environment and tools
#-----------------------------------------------------------------------------
.PHONY: show.env show.goenv

show.env: ; $(info $(H) environment variables...)
	$(Q) printenv

show.goenv: ; $(info $(H) go environment...)
	$(Q) $(GO) version
	$(Q) $(GO) env

# tickle
# show makefile variables. Usage: make show.<variable-name>
show.%: ; $(info $* $(H) $($*))
	$(Q) true

#-----------------------------------------------------------------------------
# Target: artifacts and distribution
#-----------------------------------------------------------------------------
# deb, rpm, etc packages
include tools/packaging/packaging.mk

#-----------------------------------------------------------------------------
# Target: e2e tests
#-----------------------------------------------------------------------------
include tests/istio.mk

#-----------------------------------------------------------------------------
# Target: integration tests
#-----------------------------------------------------------------------------
include tests/integration/tests.mk

include common/Makefile.common.mk
