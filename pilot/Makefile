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

.PHONY: setup
setup: kubeconfig
	@bin/install-prereqs.sh
	@bin/init.sh

.PHONY: fmt
fmt:
	@bin/fmt.sh

.PHONY: build
build:	
	@bazel build //...

.PHONY: clean
clean:
	@bazel clean

.PHONY: test
test: build
	@bazel test //...

.PHONY: coverage
coverage:
	@bin/codecov.sh

.PHONY: lint
lint: build
	@bin/check.sh

.PHONY: gazelle
gazelle:
	@bin/gazelle

.PHONY: racetest
racetest:
	@bazel test --features=race //...

kubeconfig:
	@ln -s ~/.kube/config platform/kube/

