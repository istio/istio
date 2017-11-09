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

SHELL := /bin/bash

detected_OS := $(shell sh -c 'uname -s 2>/dev/null || echo not')
BUILD_FLAGS = "--cpu=k8"
ifeq ($(detected_OS),Darwin)  # Mac OS X
    BUILD_FLAGS = --cpu=darwin
endif

hub = ""
tag = ""

ifneq ($(strip $(HUB)),)
	hub =-hub ${HUB}
endif

ifneq ($(strip $(TAG)),)
	tag =-tag ${TAG}
endif

.PHONY: setup
setup: platform/kube/config
	@bin/install-prereqs.sh
	@bin/init.sh

.PHONY: build
build:	
	@bazel build ${BUILD_FLAGS} //pilot/...

.PHONY: docker
docker:
	@bin/push-docker ${hub} ${tag} -build-only

.PHONY: docker-push
docker-push:
	@bin/push-docker ${hub} ${tag}

.PHONY: clean
clean:
	@bazel clean

.PHONY: test
test:
	@bazel test //pilot/...

.PHONY: e2etest
e2etest:
	@bazel run //pilot/test/integration -- --logtostderr ${TESTOPTS} ${hub} ${tag}

.PHONY: coverage
coverage:
	@bin/codecov.sh

.PHONY: lint
lint:
	@bin/check.sh

.PHONY: gazelle
gazelle:
	@bin/gazelle

.PHONY: racetest
racetest:
	@bazel test ${BUILD_FLAGS} --features=race //...

platform/kube/config:
	@ln -s ~/.kube/config platform/kube/

