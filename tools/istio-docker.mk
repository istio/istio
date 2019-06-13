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
docker: build test-bins docker.all

DOCKER_TARGETS:=docker.pilot docker.proxy_debug docker.proxytproxy docker.proxyv2 docker.app docker.test_policybackend \
	docker.proxy_init docker.mixer docker.mixer_codegen docker.citadel docker.galley docker.sidecar_injector docker.kubectl docker.node-agent-k8s

$(ISTIO_DOCKER) $(ISTIO_DOCKER_TAR):
	mkdir -p $@

.SECONDEXPANSION: #allow $@ to be used in dependency list

# generated content
$(ISTIO_DOCKER)/istio_ca.crt $(ISTIO_DOCKER)/istio_ca.key: ${GEN_CERT} | ${ISTIO_DOCKER}
	${GEN_CERT} --key-size=2048 --out-cert=${ISTIO_DOCKER}/istio_ca.crt \
                    --out-priv=${ISTIO_DOCKER}/istio_ca.key --organization="k8s.cluster.local" \
                    --mode=self-signed --ca=true
$(ISTIO_DOCKER)/node_agent.crt $(ISTIO_DOCKER)/node_agent.key: ${GEN_CERT} $(ISTIO_DOCKER)/istio_ca.crt $(ISTIO_DOCKER)/istio_ca.key
	${GEN_CERT} --key-size=2048 --out-cert=${ISTIO_DOCKER}/node_agent.crt \
                    --out-priv=${ISTIO_DOCKER}/node_agent.key --organization="NodeAgent" \
		    --mode=signer --host="nodeagent.google.com" --signer-cert=${ISTIO_DOCKER}/istio_ca.crt \
                    --signer-priv=${ISTIO_DOCKER}/istio_ca.key

# directives to copy files to docker scratch directory

# tell make which files are copied from $(ISTIO_OUT) and generate rules to copy them to the proper location:
# generates rules like the following:
# $(ISTIO_DOCKER)/pilot-agent: $(ISTIO_OUT)/pilot-agent | $(ISTIO_DOCKER)
# 	cp $(ISTIO_OUT)/$FILE $(ISTIO_DOCKER)/($FILE)
DOCKER_FILES_FROM_ISTIO_OUT:=pkg-test-echo-cmd-client pkg-test-echo-cmd-server \
                             pilot-discovery pilot-agent sidecar-injector mixs mixgen \
                             istio_ca node_agent node_agent_k8s galley
$(foreach FILE,$(DOCKER_FILES_FROM_ISTIO_OUT), \
        $(eval $(ISTIO_DOCKER)/$(FILE): $(ISTIO_OUT)/$(FILE) | $(ISTIO_DOCKER); cp $(ISTIO_OUT)/$(FILE) $(ISTIO_DOCKER)/$(FILE)))


# tell make which files are copied from the source tree and generate rules to copy them to the proper location:
# TODO(sdake)                      $(NODE_AGENT_TEST_FILES) $(GRAFANA_FILES)
DOCKER_FILES_FROM_SOURCE:=tools/packaging/common/istio-iptables.sh docker/ca-certificates.tgz \
                          tests/testdata/certs/cert.crt tests/testdata/certs/cert.key tests/testdata/certs/cacert.pem
# generates rules like the following:
# $(ISTIO_DOCKER)/tools/packaging/common/istio-iptables.sh: $(ISTIO_OUT)/tools/packaging/common/istio-iptables.sh | $(ISTIO_DOCKER)
# 	cp $FILE $$(@D))
$(foreach FILE,$(DOCKER_FILES_FROM_SOURCE), \
        $(eval $(ISTIO_DOCKER)/$(notdir $(FILE)): $(FILE) | $(ISTIO_DOCKER); cp $(FILE) $$(@D)))


# tell make which files are copied from ISTIO_BIN and generate rules to copy them to the proper location:
# generates rules like the following:
# $(ISTIO_DOCKER)/kubectl: $(ISTIO_BIN)/kubectl | $(ISTIO_DOCKER)
# 	cp $(ISTIO_BIN)/kubectl $(ISTIO_DOCKER)/kubectl
DOCKER_FILES_FROM_ISTIO_BIN:=kubectl
$(foreach FILE,$(DOCKER_FILES_FROM_ISTIO_BIN), \
        $(eval $(ISTIO_BIN)/$(FILE): ; bin/testEnvLocalK8S.sh getDeps))
$(foreach FILE,$(DOCKER_FILES_FROM_ISTIO_BIN), \
        $(eval $(ISTIO_DOCKER)/$(FILE): $(ISTIO_BIN)/$(FILE) | $(ISTIO_DOCKER); cp $(ISTIO_BIN)/$(FILE) $(ISTIO_DOCKER)/$(FILE)))

# pilot docker images

docker.proxy_init: pilot/docker/Dockerfile.proxy_init
docker.proxy_init: $(ISTIO_DOCKER)/istio-iptables.sh
	# Ensure ubuntu:xenial, the base image for proxy_init, is present so build doesn't fail on network hiccup
	if [[ "$(docker images -q ubuntu:xenial 2> /dev/null)" == "" ]]; then \
		docker pull ubuntu:xenial || (sleep 15 ; docker pull ubuntu:xenial) || (sleep 45 ; docker pull ubuntu:xenial) \
	fi
	$(DOCKER_RULE)

docker.sidecar_injector: pilot/docker/Dockerfile.sidecar_injector
docker.sidecar_injector:$(ISTIO_DOCKER)/sidecar-injector
	$(DOCKER_RULE)

# BUILD_PRE tells $(DOCKER_RULE) to run the command specified before executing a docker build
# BUILD_ARGS tells  $(DOCKER_RULE) to execute a docker build with the specified commands

docker.proxy_debug: BUILD_PRE=mv envoy-debug-${PROXY_REPO_SHA} envoy && chmod 755 envoy pilot-agent &&
docker.proxy_debug: BUILD_ARGS=--build-arg proxy_version=istio-proxy:${PROXY_REPO_SHA} --build-arg istio_version=${VERSION} --build-arg ISTIO_API_SHA=${ISTIO_PROXY_ISTIO_API_SHA_LABEL} --build-arg ENVOY_SHA=${ISTIO_PROXY_ENVOY_SHA_LABEL}
docker.proxy_debug: pilot/docker/Dockerfile.proxy_debug
docker.proxy_debug: tools/packaging/common/envoy_bootstrap_v2.json
docker.proxy_debug: tools/packaging/common/envoy_bootstrap_drain.json
docker.proxy_debug: install/gcp/bootstrap/gcp_envoy_bootstrap.json
docker.proxy_debug: $(ISTIO_DOCKER)/ca-certificates.tgz
docker.proxy_debug: ${ISTIO_ENVOY_DEBUG_PATH}
docker.proxy_debug: $(ISTIO_OUT)/pilot-agent
docker.proxy_debug: pilot/docker/Dockerfile.proxyv2
docker.proxy_debug: pilot/docker/envoy_pilot.yaml.tmpl
docker.proxy_debug: pilot/docker/envoy_policy.yaml.tmpl
docker.proxy_debug: pilot/docker/envoy_telemetry.yaml.tmpl
	$(DOCKER_RULE)

# The file must be named 'envoy', depends on the release.
${ISTIO_ENVOY_RELEASE_DIR}/envoy: ${ISTIO_ENVOY_RELEASE_PATH}
	mkdir -p $(DOCKER_BUILD_TOP)/proxyv2
	cp ${ISTIO_ENVOY_RELEASE_PATH} ${ISTIO_ENVOY_RELEASE_DIR}/envoy

# Default proxy image.
docker.proxyv2: BUILD_PRE=chmod 755 envoy pilot-agent &&
docker.proxyv2: BUILD_ARGS=--build-arg proxy_version=istio-proxy:${PROXY_REPO_SHA} --build-arg istio_version=${VERSION} --build-arg ISTIO_API_SHA=${ISTIO_PROXY_ISTIO_API_SHA_LABEL} --build-arg ENVOY_SHA=${ISTIO_PROXY_ENVOY_SHA_LABEL}
docker.proxyv2: tools/packaging/common/envoy_bootstrap_v2.json
docker.proxyv2: tools/packaging/common/envoy_bootstrap_drain.json
docker.proxyv2: install/gcp/bootstrap/gcp_envoy_bootstrap.json
docker.proxyv2: $(ISTIO_DOCKER)/ca-certificates.tgz
docker.proxyv2: $(ISTIO_ENVOY_RELEASE_DIR)/envoy
docker.proxyv2: $(ISTIO_OUT)/pilot-agent
docker.proxyv2: pilot/docker/Dockerfile.proxyv2
docker.proxyv2: pilot/docker/envoy_pilot.yaml.tmpl
docker.proxyv2: pilot/docker/envoy_policy.yaml.tmpl
docker.proxyv2: tools/packaging/common/istio-iptables.sh
docker.proxyv2: pilot/docker/envoy_telemetry.yaml.tmpl
	$(DOCKER_RULE)

# Proxy using TPROXY interception - but no core dumps
docker.proxytproxy: BUILD_ARGS=--build-arg proxy_version=istio-proxy:${PROXY_REPO_SHA} --build-arg istio_version=${VERSION} --build-arg ISTIO_API_SHA=${ISTIO_PROXY_ISTIO_API_SHA_LABEL} --build-arg ENVOY_SHA=${ISTIO_PROXY_ENVOY_SHA_LABEL}
docker.proxytproxy: tools/packaging/common/envoy_bootstrap_v2.json
docker.proxytproxy: tools/packaging/common/envoy_bootstrap_drain.json
docker.proxytproxy: install/gcp/bootstrap/gcp_envoy_bootstrap.json
docker.proxytproxy: $(ISTIO_DOCKER)/ca-certificates.tgz
docker.proxytproxy: $(ISTIO_ENVOY_RELEASE_DIR)/envoy
docker.proxytproxy: $(ISTIO_OUT)/pilot-agent
docker.proxytproxy: pilot/docker/Dockerfile.proxytproxy
docker.proxytproxy: pilot/docker/envoy_pilot.yaml.tmpl
docker.proxytproxy: pilot/docker/envoy_policy.yaml.tmpl
docker.proxytproxy: tools/packaging/common/istio-iptables.sh
docker.proxytproxy: pilot/docker/envoy_telemetry.yaml.tmpl
	$(DOCKER_RULE)

docker.pilot: $(ISTIO_OUT)/pilot-discovery
docker.pilot: tests/testdata/certs/cacert.pem
docker.pilot: pilot/docker/Dockerfile.pilot
	$(DOCKER_RULE)

# Test application
docker.app: tests/docker/Dockerfile.app
docker.app: $(ISTIO_OUT)/pkg-test-echo-cmd-client
docker.app: $(ISTIO_OUT)/pkg-test-echo-cmd-server
docker.app: tests/testdata/certs/cert.crt
docker.app: tests/testdata/certs/cert.key
	mkdir -p $(ISTIO_DOCKER)/testapp
	cp $^ $(ISTIO_DOCKER)/testapp
ifeq ($(DEBUG_IMAGE),1)
	# It is extremely helpful to debug from the test app. The savings in size are not worth the
	# developer pain
	cp $(ISTIO_DOCKER)/testapp/Dockerfile.app $(ISTIO_DOCKER)/testapp/Dockerfile.appdbg
	sed -e "s,FROM scratch,FROM $(HUB)/proxy_debug:$(TAG)," $(ISTIO_DOCKER)/testapp/Dockerfile.appdbg > $(ISTIO_DOCKER)/testapp/Dockerfile.appd
endif
	time (cd $(ISTIO_DOCKER)/testapp && \
		docker build -t $(HUB)/app:$(TAG) -f Dockerfile.app .)

# Test policy backend for mixer integration
docker.test_policybackend: mixer/docker/Dockerfile.test_policybackend
docker.test_policybackend: $(ISTIO_OUT)/mixer-test-policybackend
	$(DOCKER_RULE)

docker.kubectl: docker/Dockerfile$$(suffix $$@)
	$(DOCKER_RULE)

# mixer docker images

docker.mixer: mixer/docker/Dockerfile.mixer
docker.mixer: $(ISTIO_DOCKER)/mixs
docker.mixer: $(ISTIO_DOCKER)/ca-certificates.tgz
	$(DOCKER_RULE)

# mixer codegen docker images
docker.mixer_codegen: mixer/docker/Dockerfile.mixer_codegen
docker.mixer_codegen: $(ISTIO_DOCKER)/mixgen
	$(DOCKER_RULE)

# galley docker images

docker.galley: galley/docker/Dockerfile.galley
docker.galley: $(ISTIO_DOCKER)/galley
	$(DOCKER_RULE)

# security docker images

docker.citadel: security/docker/Dockerfile.citadel
docker.citadel: $(ISTIO_DOCKER)/istio_ca
docker.citadel: $(ISTIO_DOCKER)/ca-certificates.tgz
	$(DOCKER_RULE)

docker.citadel-test: security/docker/Dockerfile.citadel-test
docker.citadel-test: $(ISTIO_DOCKER)/istio_ca
docker.citadel-test: $(ISTIO_DOCKER)/istio_ca.crt
docker.citadel-test: $(ISTIO_DOCKER)/istio_ca.key
	$(DOCKER_RULE)

docker.node-agent: security/docker/Dockerfile.node-agent
docker.node-agent: $(ISTIO_DOCKER)/node_agent
	$(DOCKER_RULE)

docker.node-agent-k8s: security/docker/Dockerfile.node-agent-k8s
docker.node-agent-k8s: $(ISTIO_DOCKER)/node_agent_k8s
	$(DOCKER_RULE)

docker.node-agent-test: security/docker/Dockerfile.node-agent-test
docker.node-agent-test: $(ISTIO_DOCKER)/node_agent
docker.node-agent-test: $(ISTIO_DOCKER)/istio_ca.crt
docker.node-agent-test: $(ISTIO_DOCKER)/node_agent.crt
docker.node-agent-test: $(ISTIO_DOCKER)/node_agent.key
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

DOCKER_RULE=time (mkdir -p $(DOCKER_BUILD_TOP)/$@ && cp -r $^ $(DOCKER_BUILD_TOP)/$@ && cd $(DOCKER_BUILD_TOP)/$@ && $(BUILD_PRE) docker build $(BUILD_ARGS) -t $(HUB)/$(subst docker.,,$@):$(TAG) -f Dockerfile$(suffix $@) .)

# This target will package all docker images used in test and release, without re-building
# go binaries. It is intended for CI/CD systems where the build is done in separate job.
docker.all: $(DOCKER_TARGETS)

# for each docker.XXX target create a tar.docker.XXX target that says how
# to make a $(ISTIO_OUT)/docker/XXX.tar.gz from the docker XXX image
# note that $(subst docker.,,$(TGT)) strips off the "docker." prefix, leaving just the XXX

# create a DOCKER_TAR_TARGETS that's each of DOCKER_TARGETS with a tar. prefix
DOCKER_TAR_TARGETS:=
$(foreach TGT,$(DOCKER_TARGETS),$(eval tar.$(TGT): $(TGT) | $(ISTIO_DOCKER_TAR) ; \
   time (docker save -o ${ISTIO_DOCKER_TAR}/$(subst docker.,,$(TGT)).tar $(HUB)/$(subst docker.,,$(TGT)):$(TAG) && \
         gzip ${ISTIO_DOCKER_TAR}/$(subst docker.,,$(TGT)).tar)))

# create a DOCKER_TAR_TARGETS that's each of DOCKER_TARGETS with a tar. prefix DOCKER_TAR_TARGETS:=
$(foreach TGT,$(DOCKER_TARGETS),$(eval DOCKER_TAR_TARGETS+=tar.$(TGT)))

# this target saves a tar.gz of each docker image to ${ISTIO_OUT}/docker/
docker.save: $(DOCKER_TAR_TARGETS)

# for each docker.XXX target create a push.docker.XXX target that pushes
# the local docker image to another hub
# a possible optimization is to use tag.$(TGT) as a dependency to do the tag for us
$(foreach TGT,$(DOCKER_TARGETS),$(eval push.$(TGT): | $(TGT) ; \
	time (docker push $(HUB)/$(subst docker.,,$(TGT)):$(TAG))))

# create a DOCKER_PUSH_TARGETS that's each of DOCKER_TARGETS with a push. prefix
DOCKER_PUSH_TARGETS:=
$(foreach TGT,$(DOCKER_TARGETS),$(eval DOCKER_PUSH_TARGETS+=push.$(TGT)))

# Will build and push docker images.
docker.push: $(DOCKER_PUSH_TARGETS)

# Base image for 'debug' containers.
# You can run it first to use local changes (or guarantee it is built from scratch)
docker.basedebug:
	docker build -t istionightly/base_debug -f docker/Dockerfile.xenial_debug docker/

# Run this target to generate images based on Bionic Ubuntu
# This must be run as a first step, before the 'docker' step.
docker.basedebug_bionic:
	docker build -t istionightly/base_debug_bionic -f docker/Dockerfile.bionic_debug docker/
	docker tag istionightly/base_debug_bionic istionightly/base_debug

# Run this target to generate images based on Debian Slim
# This must be run as a first step, before the 'docker' step.
docker.basedebug_deb:
	docker build -t istionightly/base_debug_deb -f docker/Dockerfile.deb_debug docker/
	docker tag istionightly/base_debug_deb istionightly/base_debug

# Job run from the nightly cron to publish an up-to-date xenial with the debug tools.
docker.push.basedebug: docker.basedebug
	docker push istionightly/base_debug:latest
