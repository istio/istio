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
define DOCKER_COMMANDS =
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

# Echo docker directory and the template to pass image name and version to for VM testing
ECHO_DOCKER ?= pkg/test/echo/docker
VM_OS_DOCKERFILE_TEMPLATE ?= Dockerfile.app_sidecar

$(ISTIO_DOCKER) $(ISTIO_DOCKER_TAR):
	mkdir -p $@

.SECONDEXPANSION: #allow $@ to be used in dependency list

# rule for the test certs.
$(ISTIO_DOCKER)/certs:
	mkdir -p $(ISTIO_DOCKER)
	cp -a tests/testdata/certs $(ISTIO_DOCKER)/.
	chmod -R o+r $(ISTIO_DOCKER)/certs

# BUILD_PRE tells $(DOCKER_RULE) to run the command specified before executing a docker build

# The file must be named 'envoy', depends on the release.
${ISTIO_ENVOY_LINUX_RELEASE_DIR}/${SIDECAR}: ${ISTIO_ENVOY_LINUX_RELEASE_PATH} ${ISTIO_ENVOY_LOCAL}
	mkdir -p $(DOCKER_BUILD_TOP)/proxyv2
ifdef DEBUG_IMAGE
	cp ${ISTIO_ENVOY_LINUX_DEBUG_PATH} ${ISTIO_ENVOY_LINUX_RELEASE_DIR}/${SIDECAR}
else ifdef ISTIO_ENVOY_LOCAL
	# Replace the downloaded envoy with a local Envoy for proxy container build.
	# This will require addtional volume mount if build runs in container using `CONDITIONAL_HOST_MOUNTS`.
	# e.g. CONDITIONAL_HOST_MOUNTS="--mount type=bind,source=<path-to-envoy>,destination=/envoy" ISTIO_ENVOY_LOCAL=/envoy
	cp ${ISTIO_ENVOY_LOCAL} ${ISTIO_ENVOY_LINUX_RELEASE_DIR}/${SIDECAR}
else
	cp ${ISTIO_ENVOY_LINUX_RELEASE_PATH} ${ISTIO_ENVOY_LINUX_RELEASE_DIR}/${SIDECAR}
endif

# The file must be named 'envoy_bootstrap.json' because Dockerfile.proxyv2 hard-codes this.
${ISTIO_ENVOY_BOOTSTRAP_CONFIG_DIR}/envoy_bootstrap.json: ${ISTIO_ENVOY_BOOTSTRAP_CONFIG_PATH}
	cp ${ISTIO_ENVOY_BOOTSTRAP_CONFIG_PATH} ${ISTIO_ENVOY_BOOTSTRAP_CONFIG_DIR}/envoy_bootstrap.json

# rule for wasm extensions.
$(ISTIO_ENVOY_LINUX_RELEASE_DIR)/stats-filter.wasm: init
$(ISTIO_ENVOY_LINUX_RELEASE_DIR)/stats-filter.compiled.wasm: init
$(ISTIO_ENVOY_LINUX_RELEASE_DIR)/metadata-exchange-filter.wasm: init
$(ISTIO_ENVOY_LINUX_RELEASE_DIR)/metadata-exchange-filter.compiled.wasm: init

# $@ is the name of the target
# $^ the name of the dependencies for the target
# DOCKER_RULE copies all dependencies for the dockerfile into a single folder.
# This allows minimizing the inputs to the docker context
DOCKER_RULE ?= ./tools/docker-copy.sh $^ $(DOCKERX_BUILD_TOP)/$@
# RENAME_TEMPLATE clones the common VM dockerfile template to the OS specific variant.
# This allows us to have a per OS build without a ton of Dockerfiles.
RENAME_TEMPLATE ?= mkdir -p $(DOCKERX_BUILD_TOP)/$@ && cp $(ECHO_DOCKER)/$(VM_OS_DOCKERFILE_TEMPLATE) $(DOCKERX_BUILD_TOP)/$@/Dockerfile$(suffix $@)


### Dockerfile builders ###
# Unlike standard docker image builder, we use some special logic here to explicitly declare dependencies as make targets
# Any files referenced from the Dockerfile must be included as dependency for the target to be included

build.docker.proxyv2: ${ISTIO_ENVOY_BOOTSTRAP_CONFIG_DIR}/envoy_bootstrap.json
build.docker.proxyv2: ${ISTIO_ENVOY_BOOTSTRAP_CONFIG_DIR}/gcp_envoy_bootstrap.json
build.docker.proxyv2: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/${SIDECAR}
build.docker.proxyv2: $(ISTIO_OUT_LINUX)/pilot-agent
build.docker.proxyv2: pilot/docker/Dockerfile.proxyv2
build.docker.proxyv2: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/stats-filter.wasm
build.docker.proxyv2: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/stats-filter.compiled.wasm
build.docker.proxyv2: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/metadata-exchange-filter.wasm
build.docker.proxyv2: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/metadata-exchange-filter.compiled.wasm
	$(DOCKER_RULE)

build.docker.pilot: ${ISTIO_ENVOY_BOOTSTRAP_CONFIG_DIR}/envoy_bootstrap.json
build.docker.pilot: ${ISTIO_ENVOY_BOOTSTRAP_CONFIG_DIR}/gcp_envoy_bootstrap.json
build.docker.pilot: $(ISTIO_OUT_LINUX)/pilot-discovery
build.docker.pilot: pilot/docker/Dockerfile.pilot
	$(DOCKER_RULE)

# Test application
build.docker.app: $(ECHO_DOCKER)/Dockerfile.app
build.docker.app: $(ISTIO_OUT_LINUX)/client
build.docker.app: $(ISTIO_OUT_LINUX)/server
build.docker.app: $(ISTIO_DOCKER)/certs
	$(DOCKER_RULE)

# Test application bundled with the sidecar with ubuntu:xenial (for non-k8s).
build.docker.app_sidecar_ubuntu_xenial: tools/packaging/common/envoy_bootstrap.json
build.docker.app_sidecar_ubuntu_xenial: $(ISTIO_OUT_LINUX)/release/istio-sidecar.deb
build.docker.app_sidecar_ubuntu_xenial: $(ISTIO_DOCKER)/certs
build.docker.app_sidecar_ubuntu_xenial: pkg/test/echo/docker/echo-start.sh
build.docker.app_sidecar_ubuntu_xenial: $(ISTIO_OUT_LINUX)/client
build.docker.app_sidecar_ubuntu_xenial: $(ISTIO_OUT_LINUX)/server
	$(RENAME_TEMPLATE)
	$(DOCKER_RULE)

# Test application bundled with the sidecar with ubuntu:bionic (for non-k8s).
build.docker.app_sidecar_ubuntu_bionic: tools/packaging/common/envoy_bootstrap.json
build.docker.app_sidecar_ubuntu_bionic: $(ISTIO_OUT_LINUX)/release/istio-sidecar.deb
build.docker.app_sidecar_ubuntu_bionic: $(ISTIO_DOCKER)/certs
build.docker.app_sidecar_ubuntu_bionic: pkg/test/echo/docker/echo-start.sh
build.docker.app_sidecar_ubuntu_bionic: $(ISTIO_OUT_LINUX)/client
build.docker.app_sidecar_ubuntu_bionic: $(ISTIO_OUT_LINUX)/server
	$(RENAME_TEMPLATE)
	$(DOCKER_RULE)

# Test application bundled with the sidecar with ubuntu:focal (for non-k8s).
build.docker.app_sidecar_ubuntu_focal: tools/packaging/common/envoy_bootstrap.json
build.docker.app_sidecar_ubuntu_focal: $(ISTIO_OUT_LINUX)/release/istio-sidecar.deb
build.docker.app_sidecar_ubuntu_focal: $(ISTIO_DOCKER)/certs
build.docker.app_sidecar_ubuntu_focal: pkg/test/echo/docker/echo-start.sh
build.docker.app_sidecar_ubuntu_focal: $(ISTIO_OUT_LINUX)/client
build.docker.app_sidecar_ubuntu_focal: $(ISTIO_OUT_LINUX)/server
	$(RENAME_TEMPLATE)
	$(DOCKER_RULE)

# Test application bundled with the sidecar with debian 9 (for non-k8s).
build.docker.app_sidecar_debian_9: tools/packaging/common/envoy_bootstrap.json
build.docker.app_sidecar_debian_9: $(ISTIO_OUT_LINUX)/release/istio-sidecar.deb
build.docker.app_sidecar_debian_9: $(ISTIO_DOCKER)/certs
build.docker.app_sidecar_debian_9: pkg/test/echo/docker/echo-start.sh
build.docker.app_sidecar_debian_9: $(ISTIO_OUT_LINUX)/client
build.docker.app_sidecar_debian_9: $(ISTIO_OUT_LINUX)/server
	$(RENAME_TEMPLATE)
	$(DOCKER_RULE)

# Test application bundled with the sidecar with debian 10 (for non-k8s).
build.docker.app_sidecar_debian_10: tools/packaging/common/envoy_bootstrap.json
build.docker.app_sidecar_debian_10: $(ISTIO_OUT_LINUX)/release/istio-sidecar.deb
build.docker.app_sidecar_debian_10: $(ISTIO_DOCKER)/certs
build.docker.app_sidecar_debian_10: pkg/test/echo/docker/echo-start.sh
build.docker.app_sidecar_debian_10: $(ISTIO_OUT_LINUX)/client
build.docker.app_sidecar_debian_10: $(ISTIO_OUT_LINUX)/server
	$(RENAME_TEMPLATE)
	$(DOCKER_RULE)

# Test application bundled with the sidecar (for non-k8s).
build.docker.app_sidecar_centos_8: tools/packaging/common/envoy_bootstrap.json
build.docker.app_sidecar_centos_8: $(ISTIO_OUT_LINUX)/release/istio-sidecar.rpm
build.docker.app_sidecar_centos_8: $(ISTIO_DOCKER)/certs
build.docker.app_sidecar_centos_8: pkg/test/echo/docker/echo-start.sh
build.docker.app_sidecar_centos_8: $(ISTIO_OUT_LINUX)/client
build.docker.app_sidecar_centos_8: $(ISTIO_OUT_LINUX)/server
build.docker.app_sidecar_centos_8: pkg/test/echo/docker/Dockerfile.app_sidecar_centos_8
	$(DOCKER_RULE)

# Test application bundled with the sidecar (for non-k8s).
build.docker.app_sidecar_centos_7: tools/packaging/common/envoy_bootstrap.json
build.docker.app_sidecar_centos_7: $(ISTIO_OUT_LINUX)/release/istio-sidecar-centos-7.rpm
build.docker.app_sidecar_centos_7: $(ISTIO_DOCKER)/certs
build.docker.app_sidecar_centos_7: pkg/test/echo/docker/echo-start.sh
build.docker.app_sidecar_centos_7: $(ISTIO_OUT_LINUX)/client
build.docker.app_sidecar_centos_7: $(ISTIO_OUT_LINUX)/server
build.docker.app_sidecar_centos_7: pkg/test/echo/docker/Dockerfile.app_sidecar_centos_7
	$(DOCKER_RULE)

build.docker.istioctl: istioctl/docker/Dockerfile.istioctl
build.docker.istioctl: $(ISTIO_OUT_LINUX)/istioctl
	$(DOCKER_RULE)

build.docker.operator: manifests
build.docker.operator: operator/docker/Dockerfile.operator
build.docker.operator: $(ISTIO_OUT_LINUX)/operator
	$(DOCKER_RULE)

build.docker.install-cni: $(ISTIO_OUT_LINUX)/istio-cni
build.docker.install-cni: $(ISTIO_OUT_LINUX)/istio-iptables
build.docker.install-cni: $(ISTIO_OUT_LINUX)/install-cni
build.docker.install-cni: $(ISTIO_OUT_LINUX)/istio-cni-taint
build.docker.install-cni: cni/deployments/kubernetes/Dockerfile.install-cni
	$(DOCKER_RULE)

### Base images ###
build.docker.base: docker/Dockerfile.base
	$(DOCKER_RULE)
build.docker.distroless: docker/Dockerfile.distroless
	$(DOCKER_RULE)

# VM Base images
build.docker.app_sidecar_base_debian_9: VM_OS_DOCKERFILE_TEMPLATE=Dockerfile.app_sidecar_base
build.docker.app_sidecar_base_debian_9: pkg/test/echo/docker/Dockerfile.app_sidecar_base
	$(RENAME_TEMPLATE)
	$(DOCKER_RULE)

build.docker.app_sidecar_base_debian_10: VM_OS_DOCKERFILE_TEMPLATE=Dockerfile.app_sidecar_base
build.docker.app_sidecar_base_debian_10: pkg/test/echo/docker/Dockerfile.app_sidecar_base
	$(RENAME_TEMPLATE)
	$(DOCKER_RULE)

build.docker.app_sidecar_base_ubuntu_xenial: VM_OS_DOCKERFILE_TEMPLATE=Dockerfile.app_sidecar_base
build.docker.app_sidecar_base_ubuntu_xenial: pkg/test/echo/docker/Dockerfile.app_sidecar_base
	$(RENAME_TEMPLATE)
	$(DOCKER_RULE)

build.docker.app_sidecar_base_ubuntu_bionic: VM_OS_DOCKERFILE_TEMPLATE=Dockerfile.app_sidecar_base
build.docker.app_sidecar_base_ubuntu_bionic: pkg/test/echo/docker/Dockerfile.app_sidecar_base
	$(RENAME_TEMPLATE)
	$(DOCKER_RULE)

build.docker.app_sidecar_base_ubuntu_focal: VM_OS_DOCKERFILE_TEMPLATE=Dockerfile.app_sidecar_base
build.docker.app_sidecar_base_ubuntu_focal: pkg/test/echo/docker/Dockerfile.app_sidecar_base
	$(RENAME_TEMPLATE)
	$(DOCKER_RULE)

build.docker.app_sidecar_base_centos_8: VM_OS_DOCKERFILE_TEMPLATE=Dockerfile.app_sidecar_base_centos
build.docker.app_sidecar_base_centos_8: pkg/test/echo/docker/Dockerfile.app_sidecar_base_centos
	$(RENAME_TEMPLATE)
	$(DOCKER_RULE)

build.docker.app_sidecar_base_centos_7: VM_OS_DOCKERFILE_TEMPLATE=Dockerfile.app_sidecar_base_centos
build.docker.app_sidecar_base_centos_7: pkg/test/echo/docker/Dockerfile.app_sidecar_base_centos
	$(RENAME_TEMPLATE)
	$(DOCKER_RULE)
### END Base Images ###
