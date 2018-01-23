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

# Note that disabling cgo here adversely affects go get.  Instead we'll rely on this
# to be handled in bin/gobuild.sh
# export CGO_ENABLED=0

GO ?= go

export GOARCH ?= amd64

LOCAL_OS := $(shell uname)
ifeq ($(LOCAL_OS),Linux)
   export GOOS ?= linux
else ifeq ($(LOCAL_OS),Darwin)
   export GOOS ?= darwin
else
   $(error "$(LOCAL_OS) isn't recognized/supported")
   # export GOOS ?= windows
endif

ifeq ($(GOOS),linux)
  OS_DIR:=lx
else ifeq ($(GOOS),darwin)
  OS_DIR:=mac
else ifeq ($(GOOS),windows)
  OS_DIR:=win
else
   $(error "$(GOOS) isn't recognized/supported")
endif

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
ISTIO_OUT:=$(GO_TOP)/out/$(OS_DIR)/$(GOARCH)/$(BUILDTYPE_DIR)
ISTIO_DOCKER:=${ISTIO_OUT}/docker

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
DEP    := $(shell which dep    || echo "${ISTIO_BIN}/dep" )
GOLINT := $(shell which golint || echo "${ISTIO_BIN}/golint" )

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

$(ISTIO_OUT):
	mkdir -p $(@)

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
	unset GOOS && go get -u github.com/golang/lint/golint

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

DIRS_TO_CLEAN:=
FILES_TO_CLEAN:=

clean: clean.go
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
	cp ${ISTIO_OUT}/{pilot-discovery,pilot-agent,sidecar-injector} pilot/docker
	time (cd pilot/docker && docker build -t ${HUB}/proxy_debug:${TAG} -f Dockerfile.proxy_debug .)
	time (cd pilot/docker && docker build -t ${HUB}/proxy_init:${TAG} -f Dockerfile.proxy_init .)
	time (cd pilot/docker && docker build -t ${HUB}/sidecar_injector:${TAG} -f Dockerfile.sidecar_injector .)
	time (cd pilot/docker && docker build -t ${HUB}/pilot:${TAG} -f Dockerfile.pilot .)
	cp ${ISTIO_OUT}/mixs mixer/docker
	cp docker/ca-certificates.tgz mixer/docker
	time (cd mixer/docker && docker build -t ${HUB}/mixer_debug:${TAG} -f Dockerfile.debug .)
	cp ${ISTIO_OUT}/{istio_ca,node_agent} security/docker
	cp docker/ca-certificates.tgz security/docker/
	time (cd security/docker && docker build -t ${HUB}/istio-ca:${TAG} -f Dockerfile.istio-ca .)
	time (cd security/docker && docker build -t ${HUB}/node-agent:${TAG} -f Dockerfile.node-agent .)
	cp ${ISTIO_OUT}/{pilot-test-client,pilot-test-server,pilot-test-eurekamirror} pilot/docker
	time (cd pilot/docker && docker build -t ${HUB}/app:${TAG} -f Dockerfile.app .)
	time (cd pilot/docker && docker build -t ${HUB}/eurekamirror:${TAG} -f Dockerfile.eurekamirror .)
	# TODO: generate or checkin test CA and keys
	## These are not used so far
	security/bin/gen-keys.sh
	time (cd security/docker && docker build -t ${HUB}/istio-ca-test:${TAG} -f Dockerfile.istio-ca-test .)
	time (cd security/docker && docker build -t ${HUB}/node-agent-test:${TAG} -f Dockerfile.node-agent-test .)

##################################################################################

# for now docker is limited to Linux compiles
ifeq ($(GOOS),linux)

$(ISTIO_DOCKER):
	mkdir -p $(@)

DIRS_TO_CLEAN+=$(ISTIO_DOCKER)

.SECONDEXPANSION: #allow $@ to be used in dependency list

# static files/directories that are copied from source tree

PROXY_JSON_FILES:=pilot/docker/envoy_pilot.json \
                  pilot/docker/envoy_pilot_auth.json \
                  pilot/docker/envoy_mixer.json \
                  pilot/docker/envoy_mixer_auth.json

NODE_AGENT_TEST_FILES:=security/docker/start_app.sh \
                       security/docker/app.js \
                       security/docker/istio_ca.crt \
                       security/docker/node_agent.crt \
                       security/docker/node_agent.key

GRAFANA_FILES:=mixer/deploy/kube/conf/import_dashboard.sh \
               mixer/deploy/kube/conf/start.sh \
               mixer/deploy/kube/conf/grafana-dashboard.json \
               mixer/deploy/kube/conf/mixer-dashboard.json \
               mixer/deploy/kube/conf/pilot-dashboard.json

# copied/generated files for docker build

.SECONDEXPANSION: #allow $@ to be used in dependency list

# each of these files .../XXX is copied from ${ISTIO_BIN}/XXX
# NOTE: each of these are passed to rm -f during "make clean".  Keep in mind
# cases where you might change any of these to be a new or former source code path
COPIED_FROM_ISTIO_BIN:=pilot/docker/pilot-agent pilot/docker/pilot-discovery \
                       pilot/docker/pilot-test-client pilot/docker/pilot-test-server \
                       pilot/docker/sidecar-injector pilot/docker/pilot-test-eurekamirror \
                       mixer/docker/mixs mixer/example/servicegraph/docker/servicegraph \
                       security/docker/istio_ca security/docker/node_agent

$(COPIED_FROM_ISTIO_BIN): ${ISTIO_BIN}/$$(@F)
	cp $< $(@D)

# note that "viz" is a directory rather than a file
$(ISTIO_DOCKER)/viz: mixer/example/servicegraph/js/viz | $(ISTIO_DOCKER)
	cp -r $< $(@D)

# generated content

# tell make which files are generated by gen-keys.sh
GENERATED_CERT_FILES:=istio_ca.crt istio_ca.key node_agent.crt node_agent.key
$(foreach TGT,$(GENERATED_CERT_FILES),$(eval $(ISTIO_DOCKER)/$(TGT): security/bin/gen-keys.sh | $(ISTIO_DOCKER); \
	OUTPUT_DIR=$(ISTIO_DOCKER) security/bin/gen-keys.sh))

# directives to copy files to docker scratch directory

# tell make which files are copied form go/bin
DOCKER_FILES_FROM_ISTIO_BIN:=pilot-test-client pilot-test-server pilot-test-eurekamirror \
                             pilot-discovery pilot-agent sidecar-initializer servicegraph mixs \
                             istio_ca node_agent
$(foreach FILE,$(DOCKER_FILES_FROM_ISTIO_BIN), \
        $(eval $(ISTIO_DOCKER)/$(FILE): $(ISTIO_OUT)/$(FILE) | $(ISTIO_DOCKER); cp $$< $$(@D)))

# tell make which files are copied from the source tree
DOCKER_FILES_FROM_SOURCE:=pilot/docker/prepare_proxy.sh docker/ca-certificates.tgz \
                          $(PROXY_JSON_FILES) $(NODE_AGENT_TEST_FILES)
$(foreach FILE,$(DOCKER_FILES_FROM_SOURCE), \
        $(eval $(ISTIO_DOCKER)/$(notdir $(FILE)): $(FILE) | $(ISTIO_DOCKER); cp $(FILE) $$(@D)))

# This block exists temporarily.  These files need to go in a certs/ subdir, though
# eventually the dockerfile should be changed to not used the subdir, in which case
# the files listed in this section can be added to the preceeding section
$(ISTIO_DOCKER)/certs: ; mkdir -p $(@)
DOCKER_CERTS_FILES_FROM_SOURCE:=pilot/docker/certs/cert.crt pilot/docker/certs/cert.key
$(foreach FILE,$(DOCKER_CERTS_FILES_FROM_SOURCE), \
        $(eval $(ISTIO_DOCKER)/certs/$(notdir $(FILE)): $(FILE) | $(ISTIO_DOCKER)/certs; cp $(FILE) $$(@D)))

# pilot docker images

docker.app: $(ISTIO_DOCKER)/pilot-test-client $(ISTIO_DOCKER)/pilot-test-server \
            $(ISTIO_DOCKER)/certs/cert.crt $(ISTIO_DOCKER)/certs/cert.key
docker.eurekamirror: $(ISTIO_DOCKER)/pilot-test-eurekamirror
docker.pilot:        $(ISTIO_DOCKER)/pilot-discovery
docker.proxy docker.proxy_debug: $(ISTIO_DOCKER)/pilot-agent
$(foreach FILE,$(PROXY_JSON_FILES),$(eval docker.proxy docker.proxy_debug: $(ISTIO_DOCKER)/$(notdir $(FILE))))
docker.proxy_init: $(ISTIO_DOCKER)/prepare_proxy.sh
docker.sidecar_initializer: $(ISTIO_DOCKER)/sidecar-injector

PILOT_DOCKER:=docker.app docker.eurekamirror docker.pilot docker.proxy \
              docker.proxy_debug docker.proxy_init docker.sidecar_injector
$(PILOT_DOCKER): pilot/docker/Dockerfile$$(suffix $$@) | $(ISTIO_DOCKER)
	$(DOCKER_SPECIFIC_RULE)

# mixer/example docker images

# Note that Dockerfile and Dockerfile.debug are too generic for parallel builds
SERVICEGRAPH_DOCKER:=docker.servicegraph docker.servicegraph_debug
$(SERVICEGRAPH_DOCKER): mixer/example/servicegraph/docker/Dockerfile$$(if $$(findstring debug,$$@),.debug) \
		$(ISTIO_DOCKER)/servicegraph $(ISTIO_DOCKER)/viz | $(ISTIO_DOCKER)
	$(DOCKER_GENERIC_RULE)

# mixer docker images

# Note that Dockerfile and Dockerfile.debug are too generic for parallel builds
MIXER_DOCKER:=docker.mixer docker.mixer_debug
$(MIXER_DOCKER): mixer/docker/Dockerfile$$(if $$(findstring debug,$$@),.debug) \
		$(ISTIO_DOCKER)/ca-certificates.tgz $(ISTIO_DOCKER)/mixs | $(ISTIO_DOCKER)
	$(DOCKER_GENERIC_RULE)

# security docker images

docker.istio-ca:        $(ISTIO_DOCKER)/istio_ca     $(ISTIO_DOCKER)/ca-certificates.tgz
docker.istio-ca-test:   $(ISTIO_DOCKER)/istio_ca.crt $(ISTIO_DOCKER)/istio_ca.key
docker.node-agent:      $(ISTIO_DOCKER)/node_agent
docker.node-agent-test: $(ISTIO_DOCKER)/node_agent $(ISTIO_DOCKER)/istio_ca.key \
                        $(ISTIO_DOCKER)/node_agent.crt $(ISTIO_DOCKER)/node_agent.key
$(foreach FILE,$(NODE_AGENT_TEST_FILES),$(eval docker.node-agent-test: $(ISTIO_DOCKER)/$(notdir $(FILE))))

SECURITY_DOCKER:=docker.istio-ca docker.istio-ca-test docker.node-agent docker.node-agent-test
$(SECURITY_DOCKER): security/docker/Dockerfile$$(suffix $$@) | $(ISTIO_DOCKER)
	$(DOCKER_SPECIFIC_RULE)

# grafana image

docker.grafana: mixer/deploy/kube/conf/Dockerfile $(GRAFANA_FILES)
	time (cd mixer/deploy/kube/conf && docker build -t grafana -f Dockerfile .)

DOCKER_TARGETS:=$(PILOT_DOCKER) $(SERVICEGRAPH_DOCKER) $(MIXER_DOCKER) $(SECURITY_DOCKER) docker.grafana

# Rule used above for targets that use a Dockerfile name in the form Dockerfile.suffix
DOCKER_SPECIFIC_RULE=time (cp $< $(ISTIO_DOCKER)/ && cd $(ISTIO_DOCKER) && \
                     docker build -t $(subst docker.,,$@) -f Dockerfile$(suffix $@) .)

# Rule used above for targets that use the name Dockerfile or Dockerfile.debug .
# Note that these names overlap and thus aren't suitable for parallel builds.
# This is also why Dockerfiles are always copied (to avoid using another image's file).
DOCKER_GENERIC_RULE=time (cp $< $(ISTIO_DOCKER)/ && cd $(ISTIO_DOCKER) && \
                     docker build -t $(subst docker.,,$@) -f Dockerfile$(if $(findstring debug,$@),.debug) .)

docker.all: $(DOCKER_TARGETS)

# for each docker.XXX target create a tar.docker.XXX target that says how
# to make a $(ISTIO_OUT)/docker/XXX.tar.gz from the docker XXX image
# note that $(subst docker.,,$(TGT)) strips off the "docker." prefix, leaving just the XXX
$(foreach TGT,$(DOCKER_TARGETS),$(eval tar.$(TGT): $(TGT); \
   time (mkdir -p ${ISTIO_OUT}/docker && \
         docker save -o ${ISTIO_OUT}/docker/$(subst docker.,,$(TGT)).tar $(subst docker.,,$(TGT)) && \
         gzip ${ISTIO_OUT}/docker/$(subst docker.,,$(TGT)).tar)))

# create a DOCKER_TAR_TARGETS that's each of DOCKER_TARGETS with a tar. prefix
DOCKER_TAR_TARGETS:=
$(foreach TGT,$(DOCKER_TARGETS),$(eval DOCKER_TAR_TARGETS+=tar.$(TGT)))

# this target saves a tar.gz of each docker image to ${ISTIO_OUT}/docker/
docker.save: $(DOCKER_TAR_TARGETS)

# for each docker.XXX target create a push.docker.XXX target that pushes
# the local docker image to another hub
$(foreach TGT,$(DOCKER_TARGETS),$(eval push.$(TGT): | $(TGT) checkvars; \
        time (docker tag $(subst docker.,,$(TGT)) $(HUB)/$(subst docker.,,$(TGT)):$(TAG) && \
                    docker push $(HUB)/$(subst docker.,,$(TGT)):$(TAG))))

# create a DOCKER_PUSH_TARGETS that's each of DOCKER_TARGETS with a push. prefix
DOCKER_PUSH_TARGETS:=
$(foreach TGT,$(DOCKER_TARGETS),$(eval DOCKER_PUSH_TARGETS+=push.$(TGT)))

# This target pushes each docker image to specified HUB and TAG.
# The push scripts support a comma-separated list of HUB(s) and TAG(s),
# but I'm not sure this is worth the added complexity to support.
docker.push: $(DOCKER_PUSH_TARGETS)

endif # end of docker block that's restricted to Linux

push: checkvars installgen
	$(ISTIO_GO)/bin/push $(HUB) $(TAG) $(GS_BUCKET)

# files generated by the push-docker scripts that are run by bin/push
FILES_TO_CLEAN+=pilot/docker/pilot-test-client \
                pilot/docker/pilot-test-eurekamirror \
                pilot/docker/pilot-test-server \
                security/docker/ca-certificates.tgz

artifacts: docker
	@echo 'To be added'

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

.PHONY: artifacts build checkvars clean docker test setup push kubelink installgen

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
