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

hub = ""
tag = ""

ifneq ($(strip $(HUB)),)
	hub =-hub ${HUB}
endif

ifneq ($(strip $(TAG)),)
	tag =-tag ${TAG}
endif

checkvars:
	@if test -z "$(TAG)"; then echo "TAG missing"; exit 1; fi
	@if test -z "$(HUB)"; then echo "HUB missing"; exit 1; fi

.PHONY: test
test:
	@bazel test //...

.PHONY: docker
docker:
	@$(TOP)/security/bin/push-docker ${hub} ${tag} -build-only
	@$(TOP)/pilot/bin/push-docker ${hub} ${tag} -build-only
	@$(TOP)/mixer/bin/push-docker ${hub} ${tag} -build-only

.PHONY: push
push: checkvars
	@$(TOP)/pilot/bin/push-docker ${hub} ${tag}
	@$(TOP)/mixer/bin/push-docker ${hub} ${tag}
	@$(TOP)/security/bin/push-docker ${hub} ${tag}
	@$(TOP)/pilot/bin/upload-istioctl -p "gs://istio-artifacts/pilot/$(TAG)/artifacts/istioctl"
	@$(TOP)/pilot/bin/push-debian.sh -c opt -p "gs://istio-artifacts/pilot/$(TAG)/artifacts/debs"
	@$(TOP)/security/bin/push-debian.sh -c opt -p "gs://istio-artifacts/auth/${TAG}/artifacts/debs"
