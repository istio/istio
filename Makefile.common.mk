# WARNING: DO NOT EDIT, THIS FILE IS PROBABLY A COPY
#
# The original version of this file is located in the https://github.com/istio/common-files repo.
# If you're looking at this file in a different repo and want to make a change, please go to the
# common-files repo, make the change there and check it in. Then come back to this repo and run
# "make updatecommon".

# Copyright 2018 Istio Authors
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

IMG = docker.io/sdake/build-tools:2019-08-06
UID = $(shell id -u)
PWD = $(shell pwd)
GOBIN_SOURCE ?= $(GOPATH)/bin
GOBIN ?= /work/out/bin

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

RUN = docker run -t --sig-proxy=true -u $(UID) --rm \
	-e GOOS="$(GOOS)" \
	-e GOARCH="$(GOARCH)" \
	-v /etc/passwd:/etc/passwd:ro \
	-v $(readlink /etc/localtime):/etc/localtime:ro \
	--mount type=bind,source="$(PWD)",destination="/work" \
	--mount type=volume,source=istio-go-mod,destination="/go/pkg/mod" \
	--mount type=volume,source=istio-go-cache,destination="/gocache" \
	--mount type=bind,source="$(GOBIN_SOURCE)",destination="/go/out/bin" \
	-w /work $(IMG)

#	-v /etc/timezeone:/etc/timezeone:ro \
# Set the enviornment variable USE_LOCAL_TOOLCHAIN to 1 to use the
# systemwide toolchain. Otherwise use a fairly tidy build container to
# build the repository. In this second mode of operation, only docker
# and make are required in the environment.
export USE_LOCAL_TOOLCHAIN ?= 0
ifeq ($(USE_LOCAL_TOOLCHAIN),1)
RUN =
endif

MAKE = $(RUN) make -e -f Makefile.container.mk

.PHONY: updatecommon

updatecommon:
	@git clone https://github.com/istio/common-files
	@cd common-files
	@git rev-parse HEAD >.commonfiles.sha
	@cp -r common-files/files/* common-files/files/.[^.]* .
	@rm -fr common-files
