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

SHELL := /bin/bash

# allow optional per-repo overrides
-include Makefile.overrides.mk

# Set the environment variable BUILD_WITH_CONTAINER to use a container
# to build the repo. The only dependencies in this mode are to have make and
# docker. If you'd rather build with a local tool chain instead, you'll need to
# figure out all the tools you need in your environment to make that work.
export BUILD_WITH_CONTAINER ?= 0

ifeq ($(BUILD_WITH_CONTAINER),1)

# An export free of arugments in a Makefile places all variables in the Makefile into the
# environment. This is needed to allow overrides from Makefile.overrides.mk.
export

$(shell $(shell pwd)/common/scripts/setup_env.sh)

RUN = ./common/scripts/run.sh

MAKE_DOCKER = $(RUN) make --no-print-directory -e -f Makefile.core.mk

%:
	@$(MAKE_DOCKER) $@

default:
	@$(MAKE_DOCKER)

shell:
	@$(RUN) /bin/bash

.PHONY: default shell

else

# If we are not in build container, we need a workaround to get environment properly set
# Write to file, then include
$(shell mkdir -p out)
$(shell $(shell pwd)/common/scripts/setup_env.sh envfile > out/.env)
include out/.env
# An export free of arugments in a Makefile places all variables in the Makefile into the
# environment. This behavior may be surprising to many that use shell often, which simply
# displays the existing environment
export

export GOBIN ?= $(GOPATH)/bin
include Makefile.core.mk

endif
