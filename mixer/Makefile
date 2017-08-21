## Copyright 2016 Istio Authors
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

build: testdata/kubernetes/config
	@bazel build //...:all

clean:
	@bazel clean

test: testdata/kubernetes/config
	@bazel test //...

lint: build
	@bin/linters.sh

fmt:
	@bin/fmt.sh

coverage:
	@bin/codecov.sh

racetest:
	@bazel test --features=race //...

testdata/kubernetes/config:
	@ln -sf ~/.kube/config testdata/kubernetes/config

gazelle:
	@bin/gazelle

.PHONY: build clean test lint fmt coverage racetest gazelle
