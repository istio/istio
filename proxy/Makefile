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

TOP := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))

SHELL := /bin/bash
LOCAL_ARTIFACTS_DIR ?= $(abspath artifacts)
ARTIFACTS_DIR ?= $(LOCAL_ARTIFACTS_DIR)
BAZEL_STARTUP_ARGS ?= --batch
BAZEL_BUILD_ARGS ?=
BAZEL_TEST_ARGS ?=
HUB ?=
TAG ?=

proxy.build:
	bazel $(BAZEL_STARTUP_ARGS) build $(BAZEL_BUILD_ARGS) //proxy/... //proxy/src/envoy:envoy_tar

# Build only envoy - fast
proxy.build_envoy:
	bazel $(BAZEL_STARTUP_ARGS) build $(BAZEL_BUILD_ARGS) //proxy/src/envoy:envoy

proxy.clean:
	@bazel clean

proxy.test:
	bazel $(BAZEL_STARTUP_ARGS) test $(BAZEL_TEST_ARGS) //proxy/...

proxy.test_asan:
	CC=clang-5.0 CXX=clang++-5.0 bazel $(BAZEL_STARTUP_ARGS) test $(BAZEL_TEST_ARGS) --config=clang-asan //proxy/...

proxy.test_tsan:
	CC=clang-5.0 CXX=clang++-5.0 bazel $(BAZEL_STARTUP_ARGS) test $(BAZEL_TEST_ARGS) --config=clang-tsan //proxy/...

proxy.check:
	@proxy/script/check-license-headers
	@proxy/script/check-style

proxy.artifacts: proxy.build
	@proxy/script/push-debian.sh -c opt -p $(ARTIFACTS_DIR)

proxy.deb:
	@bazel $(BAZEL_STARTUP_ARGS) build $(BAZEL_BUILD_ARGS) //proxy/tools/deb:istio-proxy

.PHONY: proxy.build proxy.clean proxy.test proxy.check proxy.artifacts
