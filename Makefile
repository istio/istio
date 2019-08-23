# WARNING: DO NOT EDIT, THIS FILE IS PROBABLY A COPY
#
# The original version of this file is located in the https://github.com/istio/common-files repo.
# If you're looking at this file in a different repo and want to make a change, please go to the
# common-files repo, make the change there and check it in. Then come back to this repo and run
# "make update-common".

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

# allow optional per-repo overrides
-include Makefile.overrides.mk

RUN =

# Set the environment variable BUILD_WITH_CONTAINER to use a container
# to build the repo. The only dependencies in this mode are to have make and
# docker. If you'd rather build with a local tool chain instead, you'll need to
# figure out all the tools you need in your environment to make that work.
export BUILD_WITH_CONTAINER ?= 0
ifeq ($(BUILD_WITH_CONTAINER),1)
IMG = gcr.io/istio-testing/build-tools:2019-08-21T08-35-40
UID = $(shell id -u)
PWD = $(shell pwd)
GOBIN_SOURCE ?= $(GOPATH)/bin
export GOBIN ?= /work/out/bin

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
endif

export GOOS ?= $(GOOS_LOCAL)

RUN = docker run -t -i --sig-proxy=true -u $(UID) --rm \
	-e GOOS="$(GOOS)" \
	-e GOARCH="$(GOARCH)" \
	-e GOBIN="$(GOBIN)" \
	-v /etc/passwd:/etc/passwd:ro \
	-v $(readlink /etc/localtime):/etc/localtime:ro \
	$(CONTAINER_OPTIONS) \
	--mount type=bind,source="$(PWD)",destination="/work" \
	--mount type=volume,source=istio-go-mod,destination="/go/pkg/mod" \
	--mount type=volume,source=istio-go-cache,destination="/gocache" \
	--mount type=bind,source="$(GOBIN_SOURCE)",destination="/go/out/bin" \
	-w /work $(IMG)
else
export GOBIN ?= ./out/bin
endif

MAKE = $(RUN) make --no-print-directory -e -f Makefile.core.mk

%:
	@$(MAKE) $@

default:
	@$(MAKE)
