# WARNING: DO NOT EDIT, THIS FILE IS PROBABLY A COPY
#
# The original version of this file is located in the https://github.com/istio/common-files repo.
# If you're looking at this file in a different repo and want to make a change, please go to the
# common-files repo, make the change there and check it in. Then come back to this repo and run
# "make updatecommon".

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

RUN =

# Set the enviornment variable BUILD_WITH_CONTAINER to use a container
# to build the repo. The only dependencies in this mode are to have make and
# docker. If you'd rather build with a local tool chain instead, you'll need to
# figure out all the tools you need in your environment to make that work.
export BUILD_WITH_CONTAINER ?= 0
ifeq ($(BUILD_WITH_CONTAINER),1)
IMG = gcr.io/istio-testing/build-tools:2019-08-05
UID = $(shell id -u)
PWD = $(shell pwd)
GOBIN_SOURCE ?= $(GOPATH)/bin
GOBIN ?= /work/out/bin

RUN = docker run -t --sig-proxy=true -u $(UID) --rm \
	-v /etc/passwd:/etc/passwd:ro \
	-v $(readlink /etc/localtime):/etc/localtime:ro \
	--mount type=bind,source="$(PWD)",destination="/work" \
	--mount type=volume,source=istio-go-mod,destination="/go/pkg/mod" \
	--mount type=volume,source=istio-go-cache,destination="/gocache" \
	--mount type=bind,source="$(GOBIN_SOURCE)",destination="/go/out/bin" \
	-w /work $(IMG)
endif

MAKE = $(RUN) make -f Makefile.core.mk

%:
	@$(MAKE) $@

default:
	@$(MAKE)
