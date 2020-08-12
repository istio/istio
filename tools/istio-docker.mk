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

# Docker target will build the go binaries and package the docker for local testing.
# It does not upload to a registry.
docker: docker.all

# Add new docker targets to the end of the DOCKER_TARGETS list.

DOCKER_TARGETS ?= docker.pilot docker.proxyv2 docker.app docker.app_sidecar_ubuntu_xenial \
docker.app_sidecar_ubuntu_bionic docker.app_sidecar_ubuntu_focal docker.app_sidecar_debian_9 \
docker.app_sidecar_debian_10 docker.app_sidecar_centos_8 docker.test_policybackend docker.mixer docker.mixer_codegen \
docker.istioctl docker.operator docker.install-cni

# Echo docker directory and the template to pass image name and version to for VM testing
ECHO_DOCKER ?= pkg/test/echo/docker
VM_OS_DOCKERFILE_TEMPLATE ?= Dockerfile.app_sidecar

$(ISTIO_DOCKER) $(ISTIO_DOCKER_TAR):
	mkdir -p $@

.SECONDEXPANSION: #allow $@ to be used in dependency list

# directives to copy files to docker scratch directory

# tell make which files are copied from $(ISTIO_OUT_LINUX) and generate rules to copy them to the proper location:
# generates rules like the following:
# $(ISTIO_DOCKER)/pilot-agent: $(ISTIO_OUT_LINUX)/pilot-agent | $(ISTIO_DOCKER)
# 	cp $(ISTIO_OUT_LINUX)/$FILE $(ISTIO_DOCKER)/($FILE)
DOCKER_FILES_FROM_ISTIO_OUT_LINUX:=client server \
                             pilot-discovery pilot-agent mixs mixgen \
                             istioctl manager
$(foreach FILE,$(DOCKER_FILES_FROM_ISTIO_OUT_LINUX), \
        $(eval $(ISTIO_DOCKER)/$(FILE): $(ISTIO_OUT_LINUX)/$(FILE) | $(ISTIO_DOCKER); cp $(ISTIO_OUT_LINUX)/$(FILE) $(ISTIO_DOCKER)/$(FILE)))

# rule for the test certs.
$(ISTIO_DOCKER)/certs:
	mkdir -p $(ISTIO_DOCKER)
	cp -a tests/testdata/certs $(ISTIO_DOCKER)/.

# tell make which files are copied from the source tree and generate rules to copy them to the proper location:
# TODO(sdake)                      $(NODE_AGENT_TEST_FILES) $(GRAFANA_FILES)
DOCKER_FILES_FROM_SOURCE:=tests/testdata/certs/cert.crt tests/testdata/certs/cert.key tests/testdata/certs/cacert.pem
$(foreach FILE,$(DOCKER_FILES_FROM_SOURCE), \
        $(eval $(ISTIO_DOCKER)/$(notdir $(FILE)): $(FILE) | $(ISTIO_DOCKER); cp $(FILE) $$(@D)))

# BUILD_PRE tells $(DOCKER_RULE) to run the command specified before executing a docker build
# BUILD_ARGS tells  $(DOCKER_RULE) to execute a docker build with the specified commands

# The file must be named 'envoy', depends on the release.
${ISTIO_ENVOY_LINUX_RELEASE_DIR}/${SIDECAR}: ${ISTIO_ENVOY_LINUX_RELEASE_PATH}
	mkdir -p $(DOCKER_BUILD_TOP)/proxyv2
ifdef DEBUG_IMAGE
	cp ${ISTIO_ENVOY_LINUX_DEBUG_PATH} ${ISTIO_ENVOY_LINUX_RELEASE_DIR}/${SIDECAR}
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

# Default proxy image.
docker.proxyv2: BUILD_PRE=&& chmod 755 ${SIDECAR} pilot-agent
docker.proxyv2: BUILD_ARGS=--build-arg proxy_version=istio-proxy:${PROXY_REPO_SHA} --build-arg istio_version=${VERSION} --build-arg BASE_VERSION=${BASE_VERSION} --build-arg SIDECAR=${SIDECAR}
docker.proxyv2: ${ISTIO_ENVOY_BOOTSTRAP_CONFIG_DIR}/envoy_bootstrap.json
docker.proxyv2: install/gcp/bootstrap/gcp_envoy_bootstrap.json
docker.proxyv2: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/${SIDECAR}
docker.proxyv2: $(ISTIO_OUT_LINUX)/pilot-agent
docker.proxyv2: pilot/docker/Dockerfile.proxyv2
docker.proxyv2: pilot/docker/envoy_policy.yaml.tmpl
docker.proxyv2: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/stats-filter.wasm
docker.proxyv2: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/stats-filter.compiled.wasm
docker.proxyv2: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/metadata-exchange-filter.wasm
docker.proxyv2: $(ISTIO_ENVOY_LINUX_RELEASE_DIR)/metadata-exchange-filter.compiled.wasm
	$(DOCKER_RULE)

docker.pilot: BUILD_PRE=&& chmod 755 pilot-discovery cacert.pem
docker.pilot: BUILD_ARGS=--build-arg BASE_VERSION=${BASE_VERSION}
docker.pilot: $(ISTIO_OUT_LINUX)/pilot-discovery
docker.pilot: tests/testdata/certs/cacert.pem
docker.pilot: pilot/docker/Dockerfile.pilot
	$(DOCKER_RULE)

# Test application
docker.app: BUILD_PRE=&& chmod 755 server client
docker.app: BUILD_ARGS=--build-arg BASE_VERSION=${BASE_VERSION}
docker.app: $(ECHO_DOCKER)/Dockerfile.app
docker.app: $(ISTIO_OUT_LINUX)/client
docker.app: $(ISTIO_OUT_LINUX)/server
docker.app: $(ISTIO_DOCKER)/certs
	$(DOCKER_RULE)

# Test application bundled with the sidecar with ubuntu:xenial (for non-k8s).
docker.app_sidecar_ubuntu_xenial: BUILD_ARGS=--build-arg VM_IMAGE_NAME=ubuntu --build-arg VM_IMAGE_VERSION=xenial
docker.app_sidecar_ubuntu_xenial: tools/packaging/common/envoy_bootstrap.json
docker.app_sidecar_ubuntu_xenial: $(ISTIO_OUT_LINUX)/release/istio-sidecar.deb
docker.app_sidecar_ubuntu_xenial: $(ISTIO_DOCKER)/certs
docker.app_sidecar_ubuntu_xenial: pkg/test/echo/docker/echo-start.sh
docker.app_sidecar_ubuntu_xenial: $(ISTIO_OUT_LINUX)/client
docker.app_sidecar_ubuntu_xenial: $(ISTIO_OUT_LINUX)/server
	$(RENAME_TEMPLATE)
	$(DOCKER_RULE)

# Test application bundled with the sidecar with ubuntu:bionic (for non-k8s).
docker.app_sidecar_ubuntu_bionic: BUILD_ARGS=--build-arg VM_IMAGE_NAME=ubuntu --build-arg VM_IMAGE_VERSION=bionic
docker.app_sidecar_ubuntu_bionic: tools/packaging/common/envoy_bootstrap.json
docker.app_sidecar_ubuntu_bionic: $(ISTIO_OUT_LINUX)/release/istio-sidecar.deb
docker.app_sidecar_ubuntu_bionic: $(ISTIO_DOCKER)/certs
docker.app_sidecar_ubuntu_bionic: pkg/test/echo/docker/echo-start.sh
docker.app_sidecar_ubuntu_bionic: $(ISTIO_OUT_LINUX)/client
docker.app_sidecar_ubuntu_bionic: $(ISTIO_OUT_LINUX)/server
	$(RENAME_TEMPLATE)
	$(DOCKER_RULE)

# Test application bundled with the sidecar with ubuntu:focal (for non-k8s).
docker.app_sidecar_ubuntu_focal: BUILD_ARGS=--build-arg VM_IMAGE_NAME=ubuntu --build-arg VM_IMAGE_VERSION=focal
docker.app_sidecar_ubuntu_focal: tools/packaging/common/envoy_bootstrap.json
docker.app_sidecar_ubuntu_focal: $(ISTIO_OUT_LINUX)/release/istio-sidecar.deb
docker.app_sidecar_ubuntu_focal: $(ISTIO_DOCKER)/certs
docker.app_sidecar_ubuntu_focal: pkg/test/echo/docker/echo-start.sh
docker.app_sidecar_ubuntu_focal: $(ISTIO_OUT_LINUX)/client
docker.app_sidecar_ubuntu_focal: $(ISTIO_OUT_LINUX)/server
	$(RENAME_TEMPLATE)
	$(DOCKER_RULE)

# Test application bundled with the sidecar with debian 9 (for non-k8s).
docker.app_sidecar_debian_9: BUILD_ARGS=--build-arg VM_IMAGE_NAME=debian --build-arg VM_IMAGE_VERSION=9
docker.app_sidecar_debian_9: tools/packaging/common/envoy_bootstrap.json
docker.app_sidecar_debian_9: $(ISTIO_OUT_LINUX)/release/istio-sidecar.deb
docker.app_sidecar_debian_9: $(ISTIO_DOCKER)/certs
docker.app_sidecar_debian_9: pkg/test/echo/docker/echo-start.sh
docker.app_sidecar_debian_9: $(ISTIO_OUT_LINUX)/client
docker.app_sidecar_debian_9: $(ISTIO_OUT_LINUX)/server
	$(RENAME_TEMPLATE)
	$(DOCKER_RULE)

# Test application bundled with the sidecar with debian 10 (for non-k8s).
docker.app_sidecar_debian_10: BUILD_ARGS=--build-arg VM_IMAGE_NAME=debian --build-arg VM_IMAGE_VERSION=10
docker.app_sidecar_debian_10: tools/packaging/common/envoy_bootstrap.json
docker.app_sidecar_debian_10: $(ISTIO_OUT_LINUX)/release/istio-sidecar.deb
docker.app_sidecar_debian_10: $(ISTIO_DOCKER)/certs
docker.app_sidecar_debian_10: pkg/test/echo/docker/echo-start.sh
docker.app_sidecar_debian_10: $(ISTIO_OUT_LINUX)/client
docker.app_sidecar_debian_10: $(ISTIO_OUT_LINUX)/server
	$(RENAME_TEMPLATE)
	$(DOCKER_RULE)

# Test application bundled with the sidecar (for non-k8s).
docker.app_sidecar_centos_8: BUILD_ARGS=--build-arg BASE_VERSION=${BASE_VERSION}
docker.app_sidecar_centos_8: tools/packaging/common/envoy_bootstrap.json
docker.app_sidecar_centos_8: $(ISTIO_OUT_LINUX)/release/istio-sidecar.rpm
docker.app_sidecar_centos_8: $(ISTIO_DOCKER)/certs
docker.app_sidecar_centos_8: pkg/test/echo/docker/echo-start.sh
docker.app_sidecar_centos_8: $(ISTIO_OUT_LINUX)/client
docker.app_sidecar_centos_8: $(ISTIO_OUT_LINUX)/server
docker.app_sidecar_centos_8: pkg/test/echo/docker/Dockerfile.app_sidecar_centos_8
	$(DOCKER_RULE)

# Test policy backend for mixer integration
docker.test_policybackend: BUILD_ARGS=--build-arg BASE_VERSION=${BASE_VERSION}
docker.test_policybackend: mixer/docker/Dockerfile.test_policybackend
docker.test_policybackend: $(ISTIO_OUT_LINUX)/policybackend
	$(DOCKER_RULE)

docker.istioctl: BUILD_ARGS=--build-arg BASE_VERSION=${BASE_VERSION}
docker.istioctl: istioctl/docker/Dockerfile.istioctl
docker.istioctl: $(ISTIO_OUT_LINUX)/istioctl
	$(DOCKER_RULE)

docker.operator: manifests/
docker.operator: BUILD_ARGS=--build-arg BASE_VERSION=${BASE_VERSION}
docker.operator: operator/docker/Dockerfile.operator
docker.operator: $(ISTIO_OUT_LINUX)/operator
	$(DOCKER_RULE)

# mixer docker images

docker.mixer: BUILD_PRE=&& chmod 755 mixs
docker.mixer: BUILD_ARGS=--build-arg BASE_VERSION=${BASE_VERSION}
docker.mixer: mixer/docker/Dockerfile.mixer
docker.mixer: $(ISTIO_DOCKER)/mixs
	$(DOCKER_RULE)

# mixer codegen docker images
docker.mixer_codegen: BUILD_ARGS=--build-arg BASE_VERSION=${BASE_VERSION}
docker.mixer_codegen: mixer/docker/Dockerfile.mixer_codegen
docker.mixer_codegen: $(ISTIO_DOCKER)/mixgen
	$(DOCKER_RULE)

# CNI
docker.install-cni: BUILD_ARGS=--build-arg BASE_VERSION=${BASE_VERSION}
docker.install-cni: $(ISTIO_OUT_LINUX)/istio-cni
docker.install-cni: $(ISTIO_OUT_LINUX)/istio-cni-repair
docker.install-cni: $(ISTIO_OUT_LINUX)/istio-iptables
docker.install-cni: $(ISTIO_OUT_LINUX)/install-cni
docker.install-cni: cni/deployments/kubernetes/Dockerfile.install-cni
	$(DOCKER_RULE)

.PHONY: dockerx dockerx.save

# Docker has an experimental new build engine, https://github.com/docker/buildx
# This brings substantial (10x) performance improvements when building Istio
# However, its only built into docker since v19.03. Because its so new that devs are likely to not have
# this version, and because its experimental, this is not the default build method. As this matures we should migrate over.
# For performance, in CI this method is used.
# This target works by reusing the existing docker methods. Each docker target declares it's dependencies.
# We then override the docker rule and "build" all of these, where building just copies the dependencies
# We then generate a "bake" file, which defines all of the docker files in the repo
# Finally, we call `docker buildx bake` to generate the images.
dockerx: DOCKER_RULE?=mkdir -p $(DOCKERX_BUILD_TOP)/$@ && cp -r $^ $(DOCKERX_BUILD_TOP)/$@ && cd $(DOCKERX_BUILD_TOP)/$@ $(BUILD_PRE)
dockerx: RENAME_TEMPLATE?=mkdir -p $(DOCKERX_BUILD_TOP)/$@ && cp $(ECHO_DOCKER)/$(VM_OS_DOCKERFILE_TEMPLATE) $(DOCKERX_BUILD_TOP)/$@/Dockerfile$(suffix $@)
dockerx: docker | $(ISTIO_DOCKER_TAR)
dockerx:
	HUB=$(HUB) \
		TAG=$(TAG) \
		PROXY_REPO_SHA=$(PROXY_REPO_SHA) \
		VERSION=$(VERSION) \
		DOCKER_ALL_VARIANTS="$(DOCKER_ALL_VARIANTS)" \
		ISTIO_DOCKER_TAR=$(ISTIO_DOCKER_TAR) \
		BASE_VERSION=$(BASE_VERSION) \
		DOCKERX_PUSH=$(DOCKERX_PUSH) \
		./tools/buildx-gen.sh $(DOCKERX_BUILD_TOP) $(DOCKER_TARGETS)
	DOCKER_CLI_EXPERIMENTAL=enabled docker buildx bake -f $(DOCKERX_BUILD_TOP)/docker-bake.hcl $(DOCKER_BUILD_VARIANTS)

# Support individual images like `dockerx.pilot`
dockerx.%:
	@DOCKER_TARGETS=docker.$* BUILD_ALL=false $(MAKE) --no-print-directory -f Makefile.core.mk dockerx

docker.base: docker/Dockerfile.base
	$(DOCKER_RULE)

docker.distroless: docker/Dockerfile.distroless
	$(DOCKER_RULE)

# $@ is the name of the target
# $^ the name of the dependencies for the target
# Rule Steps #
##############
# 1. Make a directory $(DOCKER_BUILD_TOP)/%@
# 2. This rule uses cp to copy all dependency filenames into into $(DOCKER_BUILD_TOP/$@
# 3. This rule then changes directories to $(DOCKER_BUID_TOP)/$@
# 4. This rule runs $(BUILD_PRE) prior to any docker build and only if specified as a dependency variable
# 5. This rule finally runs docker build passing $(BUILD_ARGS) to docker if they are specified as a dependency variable

# DOCKER_BUILD_VARIANTS ?=default distroless
DOCKER_BUILD_VARIANTS ?= default
DOCKER_ALL_VARIANTS ?= default distroless
DEFAULT_DISTRIBUTION=default
DOCKER_RULE ?= $(foreach VARIANT,$(DOCKER_BUILD_VARIANTS), time (mkdir -p $(DOCKER_BUILD_TOP)/$@ && cp -r $^ $(DOCKER_BUILD_TOP)/$@ && cd $(DOCKER_BUILD_TOP)/$@ $(BUILD_PRE) && docker build $(BUILD_ARGS) --build-arg BASE_DISTRIBUTION=$(VARIANT) -t $(HUB)/$(subst docker.,,$@):$(subst -$(DEFAULT_DISTRIBUTION),,$(TAG)-$(VARIANT)) -f Dockerfile$(suffix $@) . ); )
RENAME_TEMPLATE ?= mkdir -p $(DOCKER_BUILD_TOP)/$@ && cp $(ECHO_DOCKER)/$(VM_OS_DOCKERFILE_TEMPLATE) $(DOCKER_BUILD_TOP)/$@/Dockerfile$(suffix $@)

# This target will package all docker images used in test and release, without re-building
# go binaries. It is intended for CI/CD systems where the build is done in separate job.
docker.all: $(DOCKER_TARGETS)

# for each docker.XXX target create a tar.docker.XXX target that says how
# to make a $(ISTIO_OUT_LINUX)/docker/XXX.tar.gz from the docker XXX image
# note that $(subst docker.,,$(TGT)) strips off the "docker." prefix, leaving just the XXX

# create a DOCKER_TAR_TARGETS that's each of DOCKER_TARGETS with a tar. prefix
DOCKER_TAR_TARGETS:=
$(foreach TGT,$(DOCKER_TARGETS),$(eval tar.$(TGT): $(TGT) | $(ISTIO_DOCKER_TAR) ; \
         $(foreach VARIANT,$(DOCKER_BUILD_VARIANTS), time ( \
		     docker save -o ${ISTIO_DOCKER_TAR}/$(subst docker.,,$(TGT))$(subst -$(DEFAULT_DISTRIBUTION),,-$(VARIANT)).tar $(HUB)/$(subst docker.,,$(TGT)):$(subst -$(DEFAULT_DISTRIBUTION),,$(TAG)-$(VARIANT)) && \
             gzip ${ISTIO_DOCKER_TAR}/$(subst docker.,,$(TGT))$(subst -$(DEFAULT_DISTRIBUTION),,-$(VARIANT)).tar \
			   ); \
		  )))

# create a DOCKER_TAR_TARGETS that's each of DOCKER_TARGETS with a tar. prefix DOCKER_TAR_TARGETS:=
$(foreach TGT,$(DOCKER_TARGETS),$(eval DOCKER_TAR_TARGETS+=tar.$(TGT)))

# this target saves a tar.gz of each docker image to ${ISTIO_OUT_LINUX}/docker/
dockerx.save: dockerx $(ISTIO_DOCKER_TAR)
	$(foreach TGT,$(DOCKER_TARGETS), \
	$(foreach VARIANT,$(DOCKER_BUILD_VARIANTS), time ( \
		 docker save -o ${ISTIO_DOCKER_TAR}/$(subst docker.,,$(TGT))$(subst -$(DEFAULT_DISTRIBUTION),,-$(VARIANT)).tar $(HUB)/$(subst docker.,,$(TGT)):$(subst -$(DEFAULT_DISTRIBUTION),,$(TAG)-$(VARIANT)) && \
		 gzip -f ${ISTIO_DOCKER_TAR}/$(subst docker.,,$(TGT))$(subst -$(DEFAULT_DISTRIBUTION),,-$(VARIANT)).tar \
		   ); \
	 ))

docker.save: $(DOCKER_TAR_TARGETS)

# for each docker.XXX target create a push.docker.XXX target that pushes
# the local docker image to another hub
# a possible optimization is to use tag.$(TGT) as a dependency to do the tag for us
$(foreach TGT,$(DOCKER_TARGETS),$(eval push.$(TGT): | $(TGT) ; \
	time (set -e && for distro in $(DOCKER_BUILD_VARIANTS); do tag=$(TAG)-$$$${distro}; docker push $(HUB)/$(subst docker.,,$(TGT)):$$$${tag%-$(DEFAULT_DISTRIBUTION)}; done)))

define run_vulnerability_scanning
        $(eval RESULTS_DIR := vulnerability_scan_results)
        $(eval CURL_RESPONSE := $(shell curl -s --create-dirs -o $(RESULTS_DIR)/$(1) -w "%{http_code}" http://imagescanner.cloud.ibm.com/scan?image="docker.io/$(2)")) \
        $(if $(filter $(CURL_RESPONSE), 200), (mv $(RESULTS_DIR)/$(1) $(RESULTS_DIR)/$(1).json))
endef

# create a DOCKER_PUSH_TARGETS that's each of DOCKER_TARGETS with a push. prefix
DOCKER_PUSH_TARGETS:=
$(foreach TGT,$(DOCKER_TARGETS),$(eval DOCKER_PUSH_TARGETS+=push.$(TGT)))

# Will build and push docker images.
docker.push: $(DOCKER_PUSH_TARGETS)

# Build and push docker images using dockerx
dockerx.push: dockerx
	$(foreach TGT,$(DOCKER_TARGETS), time ( \
		set -e && for distro in $(DOCKER_BUILD_VARIANTS); do tag=$(TAG)-$${distro}; docker push $(HUB)/$(subst docker.,,$(TGT)):$${tag%-$(DEFAULT_DISTRIBUTION)}; done); \
	)

# Build and push docker images using dockerx. Pushing is done inline as an optimization
# This is not done in the dockerx.push target because it requires using the docker-container driver.
# See https://github.com/docker/buildx#working-with-builder-instances for info to set this up
dockerx.pushx: DOCKERX_PUSH=true
dockerx.pushx: dockerx
	@:

# Scan images for security vulnerabilities using the ImageScanner tool
docker.scan_images: $(DOCKER_PUSH_TARGETS)
	$(foreach TGT,$(DOCKER_TARGETS),$(call run_vulnerability_scanning,$(subst docker.,,$(TGT)),$(HUB)/$(subst docker.,,$(TGT)):$(TAG)))
