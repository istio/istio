## Copyright 2018 Istio Authors
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

.PHONY: docker
.PHONY: docker.all
.PHONY: docker.save
.PHONY: docker.push

# DOCKER_TARGETS defines all known docker images
DOCKER_TARGETS ?= docker.pilot docker.proxyv2 docker.app docker.app_sidecar_ubuntu_xenial \
docker.app_sidecar_ubuntu_bionic docker.app_sidecar_ubuntu_focal docker.app_sidecar_debian_9 \
docker.app_sidecar_debian_10 docker.app_sidecar_centos_8 docker.app_sidecar_centos_7 \
docker.istioctl docker.operator docker.install-cni

### Docker commands ###
# Below provides various commands to build/push docker images.
# These are all wrappers around ./tools/docker, the binary that controls docker builds.
# Builds can also be done through direct ./tools/docker invocations.
# When using these commands the flow is:
#  1) make target calls ./tools/docker
#  2) ./tools/docker calls `make build.docker.x` targets to compute the dependencies required
#  3) ./tools/docker triggers the actual docker commands required
# As a result, there are two layers of make involved.

docker: ## Build all docker images
	DOCKER_TARGETS="$(DOCKER_TARGETS)" ./tools/docker

docker.save: ## Build docker images and save to tar.gz
	DOCKER_TARGETS="$(DOCKER_TARGETS)" ./tools/docker ./tools/docker --save

docker.push: ## Build all docker images and push to
	DOCKER_TARGETS="$(DOCKER_TARGETS)" ./tools/docker ./tools/docker --push

# Legacy command aliases
docker.all: docker
	@:
dockerx.save: docker.save
	@:
dockerx.push: docker.push
	@:
dockerx.pushx: docker.push
	@:
dockerx: docker
	@:

# Support individual images like `dockerx.pilot`

# Docker commands defines some convenience targets
define DOCKER_COMMANDS
# Build individual docker image and push it. Ex: push.docker.pilot
push.$(1): DOCKER_TARGETS=$(1)
push.$(1): docker.push
	@:

# Build individual docker image and save it. Ex: tar.docker.pilot
tar.$(1): DOCKER_TARGETS=$(1)
tar.$(1): docker.save
	@:

# Build individual docker image. Ex: docker.pilot
$(1): DOCKER_TARGETS=$(1)
$(1): docker
	@:

# Build individual docker image. Ex: dockerx.pilot
dockerx.$(1): DOCKER_TARGETS=$(1)
dockerx.$(1): docker
	@:
endef
$(foreach tgt,$(DOCKER_TARGETS),$(eval $(call DOCKER_COMMANDS,$(tgt))))
### End docker commands ###
