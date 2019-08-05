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

IMG = gcr.io/istio-testing/build-tools:2019-08-05
UID = $(shell id -u)
PWD = $(shell pwd)
GOBIN_SOURCE ?= $(GOPATH)/bin
GOBIN ?= /work/out/bin

RUN = docker run -t --sig-proxy=true -u $(UID) --rm \
	-v /etc/passwd:/etc/passwd:ro \
	-v $(reallink /etc/localtime):/etc/localtime:ro \
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

MAKE = $(RUN) make -f Makefile.container.mk

.PHONY: updatecommon

updatecommon:
	@git clone https://github.com/istio/common-files
	@cd common-files
	@git rev-parse HEAD >.commonfiles.sha
	@cp -r common-files/files/* common-files/files/.[^.]* .
	@rm -fr common-files
