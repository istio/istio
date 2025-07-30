# Copyright 2019 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

.DEFAULT_GOAL := default

# This repository has been enabled for BUILD_WITH_CONTAINER=1. Some
# test cases fail within Docker, and Mac + Docker isn't quite perfect.
# For more information see: https://github.com/istio/istio/pull/19322/

BUILD_WITH_CONTAINER ?= 1
# Container options for the build container. Can be customized for different container runtimes.
# Example for podman on macOS: CONTAINER_OPTIONS="--mount type=bind,source=/tmp,destination=/tmp --net=host --security-opt label=disable"
# Note: --security-opt label=disable disables SELinux labeling (safer than --privileged)
CONTAINER_OPTIONS ?= --mount type=bind,source=/tmp,destination=/tmp --net=host

export COMMONFILES_POSTPROCESS = tools/commonfiles-postprocess.sh

ifeq ($(BUILD_WITH_CONTAINER),1)
# create phony targets for the top-level items in the repo
PHONYS := $(shell ls | grep -v Makefile)
.PHONY: $(PHONYS)
$(PHONYS):
	@$(MAKE_DOCKER) $@
endif

# istioctl-install builds then installs istioctl into $GOPATH/BIN
# Used for debugging istioctl during dev work
.PHONY: istioctl-install
istioctl-install: istioctl-install-container
	cp out/$(TARGET_OS)_$(TARGET_ARCH)/istioctl ${GOPATH}/bin

.PHONY: coverage
coverage:
	prow/coverage.sh
