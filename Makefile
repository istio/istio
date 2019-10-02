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

TARGET_OUT ?= $(HOME)/istio_out/$(REPO_NAME)

ifeq ($(BUILD_WITH_CONTAINER),1)
$(info Building with the build container.)
RUN = ./common/scripts/run-docker.sh
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

shell:
	@mkdir -p $(TARGET_OUT)
	@$(RUN) /bin/bash

.PHONY: default
