# WARNING: DO NOT EDIT, THIS FILE IS PROBABLY A COPY
#
# The original version of this file is located in the https://github.com/istio/common-files repo.
# If you're looking at this file in a different repo and want to make a change, please go to the
# common-files repo, make the change there and check it in. Then come back to this repo and run
# "make update-common".

# Copyright Istio Authors
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

# Set the environment variable BUILD_WITH_CONTAINER to use a container
# to build the repo. The only dependencies in this mode are to have make and
# docker. If you'd rather build with a local tool chain instead, you'll need to
# figure out all the tools you need in your environment to make that work.
export BUILD_WITH_CONTAINER ?= 0

LOCAL_ARCH := $(shell uname -m)
ifeq ($(LOCAL_ARCH),x86_64)
    TARGET_ARCH ?= amd64
else ifeq ($(shell echo $(LOCAL_ARCH) | head -c 5),armv8)
    TARGET_ARCH ?= arm64
else ifeq ($(shell echo $(LOCAL_ARCH) | head -c 4),armv)
    TARGET_ARCH ?= arm
else
   $(error "This system's architecture $(LOCAL_ARCH) isn't recognized/supported")
endif

LOCAL_OS := $(shell uname)
ifeq ($(LOCAL_OS),Linux)
   TARGET_OS ?= linux
   READLINK_FLAGS="-f"
else ifeq ($(LOCAL_OS),Darwin)
   TARGET_OS ?= darwin
   READLINK_FLAGS=""
else
   $(error "This system's OS $(LOCAL_OS) isn't recognized/supported")
endif

REPO_ROOT = $(shell git rev-parse --show-toplevel)
REPO_NAME = $(shell basename $(REPO_ROOT))
TARGET_OUT ?= $(HOME)/istio_out/$(REPO_NAME)

ifeq ($(BUILD_WITH_CONTAINER),1)
CONTAINER_CLI ?= docker
DOCKER_SOCKET_MOUNT ?= -v /var/run/docker.sock:/var/run/docker.sock
IMG ?= gcr.io/istio-testing/build-tools:2019-10-08T20-57-24
UID = $(shell id -u)
PWD = $(shell pwd)

$(info Building with the build container: $(IMG).)

# Determine the timezone across various platforms to pass into the
# docker run operation. This operation assumes zoneinfo is within
# the path of the file.
TIMEZONE=`readlink $(READLINK_FLAGS) /etc/localtime | sed -e 's/^.*zoneinfo\///'`

RUN = $(CONTAINER_CLI) run --net=host -t -i --sig-proxy=true -u $(UID):docker --rm \
	-e IN_BUILD_CONTAINER="$(BUILD_WITH_CONTAINER)" \
	-e TZ="$(TIMEZONE)" \
	-e TARGET_ARCH="$(TARGET_ARCH)" \
	-e TARGET_OS="$(TARGET_OS)" \
	-v /etc/passwd:/etc/passwd:ro \
	$(DOCKER_SOCKET_MOUNT) \
	$(CONTAINER_OPTIONS) \
	--mount type=bind,source="$(PWD)",destination="/work" \
	--mount type=bind,source="$(TARGET_OUT)",destination="/targetout" \
	--mount type=volume,source=home,destination="/home" \
	-w /work $(IMG)
else
$(info Building with your local toolchain.)
RUN =
endif

MAKE = $(RUN) make --no-print-directory -e -f Makefile.core.mk

%:
	@mkdir -p $(TARGET_OUT)
	@$(MAKE) $@

default:
	@mkdir -p $(TARGET_OUT)
	@$(MAKE)

.PHONY: default
