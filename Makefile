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
TOP := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
SHELL := /bin/bash

GO ?= go

# @todo allow user to run for a single $PKG only?
PACKAGES := $(shell $(GO) list ./...)
GO_EXCLUDE := /vendor/|.pb.go|.gen.go
GO_FILES := $(shell find . -name '*.go' | grep -v -E '$(GO_EXCLUDE)')

BAZEL_STARTUP_ARGS ?=
BAZEL_BUILD_ARGS ?=
BAZEL_TEST_ARGS ?=

hub = ""
tag = ""

ifneq ($(strip $(HUB)),)
	hub =-hub ${HUB}
endif

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
# Target: precommit
#-----------------------------------------------------------------------------
.PHONY: precommit format check
.PHONY: fmt format.gofmt format.goimports format.bazel
.PHONY: check.vet check.lint

precommit: format check
format: format.goimports
fmt: format.gofmt format.goimports format.bazel # backward compatible with ./bin/fmt
check: check.vet check.lint

format.gofmt: ; $(info $(H) formatting files with go fmt...)
	$(Q) gofmt -s -w $(GO_FILES)

format.goimports: ; $(info $(H) formatting files with goimports...)
	$(Q) goimports -w -local istio.io $(GO_FILES)

format.bazel: ; $(info $(H) formatting bazel files...)
	$(eval BAZEL_FILES = $(shell git ls-files | grep -e 'BUILD' -e 'WORKSPACE' -e 'BUILD.bazel' -e '.*\.bazel' -e '.*\.bzl'))
	$(Q) buildifier -mode=fix $(BAZEL_FILES)

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

build: setup
	bazel $(BAZEL_STARTUP_ARGS) build $(BAZEL_BUILD_ARGS) //...

clean:
	@bazel clean

test: setup
	bazel $(BAZEL_STARTUP_ARGS) test $(BAZEL_TEST_ARGS) //...

docker:
	$(TOP)/security/bin/push-docker ${hub} ${tag} -build-only
	$(TOP)/pilot/bin/push-docker ${hub} ${tag} -build-only
	$(TOP)/mixer/bin/push-docker ${hub} ${tag} -build-only

push: checkvars
	$(TOP)/bin/push $(HUB) $(TAG)

artifacts: docker
	@echo 'To be added'

pilot/platform/kube/config:
	ln -s ~/.kube/config pilot/platform/kube/

.PHONY: artifacts build checkvars clean docker test setup push
