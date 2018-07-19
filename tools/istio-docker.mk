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

# Docker target will build the go binaries and package the docker for local testing.
# It does not upload to a registry.
docker: build test-bins docker.all

$(ISTIO_DOCKER) $(ISTIO_DOCKER_TAR):
	mkdir -p $@

.SECONDEXPANSION: #allow $@ to be used in dependency list

# static files/directories that are copied from source tree

NODE_AGENT_TEST_FILES:=security/docker/start_app.sh \
                       security/docker/app.js

GRAFANA_FILES:=addons/grafana/dashboards.yaml \
               addons/grafana/datasources.yaml \
               addons/grafana/grafana.ini

# note that "dashboards" is a directory rather than a file
$(ISTIO_DOCKER)/dashboards: addons/grafana/$$(notdir $$@) | $(ISTIO_DOCKER)
	cp -r $< $(@D)

# note that "js" and "force" are directories rather than a file
$(ISTIO_DOCKER)/js $(ISTIO_DOCKER)/force: addons/servicegraph/$$(notdir $$@) | $(ISTIO_DOCKER)
	cp -r $< $(@D)

# generated content
$(ISTIO_DOCKER)/istio_ca.crt $(ISTIO_DOCKER)/istio_ca.key: ${GEN_CERT} | ${ISTIO_DOCKER}
	${GEN_CERT} --key-size=2048 --out-cert=${ISTIO_DOCKER}/istio_ca.crt \
                    --out-priv=${ISTIO_DOCKER}/istio_ca.key --organization="k8s.cluster.local" \
                    --self-signed=true --ca=true
$(ISTIO_DOCKER)/node_agent.crt $(ISTIO_DOCKER)/node_agent.key: ${GEN_CERT} $(ISTIO_DOCKER)/istio_ca.crt $(ISTIO_DOCKER)/istio_ca.key
	${GEN_CERT} --key-size=2048 --out-cert=${ISTIO_DOCKER}/node_agent.crt \
                    --out-priv=${ISTIO_DOCKER}/node_agent.key --organization="NodeAgent" \
                    --host="nodeagent.google.com" --signer-cert=${ISTIO_DOCKER}/istio_ca.crt \
                    --signer-priv=${ISTIO_DOCKER}/istio_ca.key

# directives to copy files to docker scratch directory

# tell make which files are copied form go/out
DOCKER_FILES_FROM_ISTIO_OUT:=pilot-test-client pilot-test-server \
                             pilot-discovery pilot-agent sidecar-injector servicegraph mixs \
                             istio_ca node_agent galley
$(foreach FILE,$(DOCKER_FILES_FROM_ISTIO_OUT), \
        $(eval $(ISTIO_DOCKER)/$(FILE): $(ISTIO_OUT)/$(FILE) | $(ISTIO_DOCKER); cp $$< $$(@D)))

# This generates rules like:
#$(ISTIO_DOCKER)/pilot-agent: $(ISTIO_OUT)/pilot-agent | $(ISTIO_DOCKER)
# 	cp $$< $$(@D))

# tell make which files are copied from the source tree
DOCKER_FILES_FROM_SOURCE:=tools/deb/istio-iptables.sh docker/ca-certificates.tgz \
                          $(NODE_AGENT_TEST_FILES) $(GRAFANA_FILES) \
                          pilot/docker/certs/cert.crt pilot/docker/certs/cert.key pilot/docker/certs/cacert.pem
$(foreach FILE,$(DOCKER_FILES_FROM_SOURCE), \
        $(eval $(ISTIO_DOCKER)/$(notdir $(FILE)): $(FILE) | $(ISTIO_DOCKER); cp $(FILE) $$(@D)))

# pilot docker imagesDOCKER_BUILD_TOP

docker.proxy_init: $(ISTIO_DOCKER)/istio-iptables.sh
docker.sidecar_injector: $(ISTIO_DOCKER)/sidecar-injector

docker.proxy_debug: tools/deb/envoy_bootstrap_v2.json
docker.proxy_debug: ${ISTIO_ENVOY_DEBUG_PATH}
docker.proxy_debug: $(ISTIO_OUT)/pilot-agent
docker.proxy_debug: pilot/docker/Dockerfile.proxyv2
docker.proxy_debug: pilot/docker/envoy_pilot.yaml.tmpl
docker.proxy_debug: pilot/docker/envoy_policy.yaml.tmpl
docker.proxy_debug: pilot/docker/envoy_telemetry.yaml.tmpl
	mkdir -p $(DOCKER_BUILD_TOP)/proxyd
	cp ${ISTIO_ENVOY_DEBUG_PATH} $(DOCKER_BUILD_TOP)/proxyd/envoy
	cp pilot/docker/*.yaml.tmpl $(DOCKER_BUILD_TOP)/proxyd/
	# Not using $^ to avoid 2 copies of envoy
	cp tools/deb/envoy_bootstrap_v2.json tools/deb/istio-iptables.sh $(ISTIO_OUT)/pilot-agent pilot/docker/Dockerfile.proxyv2 $(DOCKER_BUILD_TOP)/proxyd/
	time (cd $(DOCKER_BUILD_TOP)/proxyd && \
		docker build \
		  --build-arg proxy_version=istio-proxy:${PROXY_REPO_SHA} \
		  --build-arg istio_version=${VERSION} \
		-t $(HUB)/proxy_debug:$(TAG) -f Dockerfile.proxyv2 .)

# The file must be named 'envoy', depends on the release.
${ISTIO_ENVOY_RELEASE_DIR}/envoy: ${ISTIO_ENVOY_RELEASE_PATH}
	mkdir -p $(DOCKER_BUILD_TOP)/proxyv2
	cp ${ISTIO_ENVOY_RELEASE_PATH} ${ISTIO_ENVOY_RELEASE_DIR}/envoy

# Default proxy image.
docker.proxyv2: tools/deb/envoy_bootstrap_v2.json
docker.proxyv2: $(ISTIO_ENVOY_RELEASE_DIR)/envoy
docker.proxyv2: $(ISTIO_OUT)/pilot-agent
docker.proxyv2: pilot/docker/Dockerfile.proxyv2
docker.proxyv2: pilot/docker/envoy_pilot.yaml.tmpl
docker.proxyv2: pilot/docker/envoy_policy.yaml.tmpl
docker.proxyv2: tools/deb/istio-iptables.sh
docker.proxyv2: pilot/docker/envoy_telemetry.yaml.tmpl
	mkdir -p $(DOCKER_BUILD_TOP)/proxyv2
	cp $^ $(DOCKER_BUILD_TOP)/proxyv2/
	time (cd $(DOCKER_BUILD_TOP)/proxyv2 && \
		docker build  \
		  --build-arg proxy_version=istio-proxy:${PROXY_REPO_SHA} \
		  --build-arg istio_version=${VERSION} \
		-t $(HUB)/proxyv2:$(TAG) -f Dockerfile.proxyv2 .)

# Proxy using TPROXY interception - but no core dumps
docker.proxytproxy: tools/deb/envoy_bootstrap_v2.json
docker.proxytproxy: $(ISTIO_ENVOY_RELEASE_DIR)/envoy
docker.proxytproxy: $(ISTIO_OUT)/pilot-agent
docker.proxytproxy: pilot/docker/Dockerfile.proxytproxy
docker.proxytproxy: pilot/docker/envoy_pilot.yaml.tmpl
docker.proxytproxy: pilot/docker/envoy_policy.yaml.tmpl
docker.proxytproxy: tools/deb/istio-iptables.sh
docker.proxytproxy: pilot/docker/envoy_telemetry.yaml.tmpl
	mkdir -p $(DOCKER_BUILD_TOP)/proxyv2
	cp $^ $(DOCKER_BUILD_TOP)/proxyv2/
	time (cd $(DOCKER_BUILD_TOP)/proxyv2 && \
		docker build  \
		  --build-arg proxy_version=istio-proxy:${PROXY_REPO_SHA} \
		  --build-arg istio_version=${VERSION} \
		-t $(HUB)/proxytproxy:$(TAG) -f Dockerfile.proxytproxy .)

push.proxytproxy: docker.proxytproxy
	docker push $(HUB)/proxytproxy:$(TAG)

docker.pilot: $(ISTIO_OUT)/pilot-discovery pilot/docker/certs/cacert.pem pilot/docker/Dockerfile.pilot
	mkdir -p $(ISTIO_DOCKER)/pilot
	cp $^ $(ISTIO_DOCKER)/pilot/
	time (cd $(ISTIO_DOCKER)/pilot && \
		docker build -t $(HUB)/pilot:$(TAG) -f Dockerfile.pilot .)

# Test app for pilot integration
docker.app: $(ISTIO_OUT)/pilot-test-client $(ISTIO_OUT)/pilot-test-server \
			pilot/docker/certs/cert.crt pilot/docker/certs/cert.key pilot/docker/Dockerfile.app
	mkdir -p $(ISTIO_DOCKER)/pilotapp
	cp $^ $(ISTIO_DOCKER)/pilotapp
ifeq ($(DEBUG_IMAGE),1)
	# It is extremely helpful to debug from the test app. The savings in size are not worth the
	# developer pain
	cp $(ISTIO_DOCKER)/pilotapp/Dockerfile.app $(ISTIO_DOCKER)/pilotapp/Dockerfile.appdbg
	sed -e "s,FROM scratch,FROM $(HUB)/proxy_debug:$(TAG)," $(ISTIO_DOCKER)/pilotapp/Dockerfile.appdbg > $(ISTIO_DOCKER)/pilotapp/Dockerfile.appd
endif
	time (cd $(ISTIO_DOCKER)/pilotapp && \
		docker build -t $(HUB)/app:$(TAG) -f Dockerfile.app .)

# Test policy backend for mixer integration
docker.test_policybackend: $(ISTIO_OUT)/mixer-test-policybackend \
			mixer/docker/Dockerfile.test_policybackend
	mkdir -p $(ISTIO_DOCKER)/test_policybackend
	cp $^ $(ISTIO_DOCKER)/test_policybackend
	time (cd $(ISTIO_DOCKER)/test_policybackend && \
		docker build -t $(HUB)/test_policybackend:$(TAG) -f Dockerfile.test_policybackend .)

PILOT_DOCKER:=docker.proxy_init docker.sidecar_injector
$(PILOT_DOCKER): pilot/docker/Dockerfile$$(suffix $$@) | $(ISTIO_DOCKER)
	$(DOCKER_RULE)

# addons docker images

SERVICEGRAPH_DOCKER:=docker.servicegraph docker.servicegraph_debug
$(SERVICEGRAPH_DOCKER): addons/servicegraph/docker/Dockerfile$$(suffix $$@) \
		$(ISTIO_DOCKER)/servicegraph $(ISTIO_DOCKER)/js $(ISTIO_DOCKER)/force | $(ISTIO_DOCKER)
	$(DOCKER_RULE)

# mixer docker images

MIXER_DOCKER:=docker.mixer docker.mixer_debug
$(MIXER_DOCKER): mixer/docker/Dockerfile$$(suffix $$@) \
		$(ISTIO_DOCKER)/ca-certificates.tgz $(ISTIO_DOCKER)/mixs | $(ISTIO_DOCKER)
	$(DOCKER_RULE)

# galley docker images

GALLEY_DOCKER:=docker.galley
$(GALLEY_DOCKER): galley/docker/Dockerfile$$(suffix $$@) $(ISTIO_DOCKER)/galley | $(ISTIO_DOCKER)
	$(DOCKER_RULE)

# security docker images

docker.citadel:         $(ISTIO_DOCKER)/istio_ca     $(ISTIO_DOCKER)/ca-certificates.tgz
docker.citadel-test:    $(ISTIO_DOCKER)/istio_ca.crt $(ISTIO_DOCKER)/istio_ca.key
docker.node-agent:      $(ISTIO_DOCKER)/node_agent
docker.node-agent-test: $(ISTIO_DOCKER)/node_agent $(ISTIO_DOCKER)/istio_ca.key \
                        $(ISTIO_DOCKER)/node_agent.crt $(ISTIO_DOCKER)/node_agent.key
$(foreach FILE,$(NODE_AGENT_TEST_FILES),$(eval docker.node-agent-test: $(ISTIO_DOCKER)/$(notdir $(FILE))))

SECURITY_DOCKER:=docker.citadel docker.citadel-test docker.node-agent docker.node-agent-test
$(SECURITY_DOCKER): security/docker/Dockerfile$$(suffix $$@) | $(ISTIO_DOCKER)
	$(DOCKER_RULE)

# grafana image

$(foreach FILE,$(GRAFANA_FILES),$(eval docker.grafana: $(ISTIO_DOCKER)/$(notdir $(FILE))))
docker.grafana: addons/grafana/Dockerfile$$(suffix $$@) $(GRAFANA_FILES) $(ISTIO_DOCKER)/dashboards
	$(DOCKER_RULE)

DOCKER_TARGETS:=docker.pilot docker.proxy_debug docker.proxyv2 docker.app docker.test_policybackend $(PILOT_DOCKER) $(SERVICEGRAPH_DOCKER) $(MIXER_DOCKER) $(SECURITY_DOCKER) docker.grafana $(GALLEY_DOCKER)

DOCKER_RULE=time (cp $< $(ISTIO_DOCKER)/ && cd $(ISTIO_DOCKER) && \
            docker build -t $(HUB)/$(subst docker.,,$@):$(TAG) -f Dockerfile$(suffix $@) .)

# This target will package all docker images used in test and release, without re-building
# go binaries. It is intended for CI/CD systems where the build is done in separate job.
docker.all: $(DOCKER_TARGETS)

# for each docker.XXX target create a tar.docker.XXX target that says how
# to make a $(ISTIO_OUT)/docker/XXX.tar.gz from the docker XXX image
# note that $(subst docker.,,$(TGT)) strips off the "docker." prefix, leaving just the XXX
$(foreach TGT,$(DOCKER_TARGETS),$(eval tar.$(TGT): $(TGT) | $(ISTIO_DOCKER_TAR) ; \
   time (docker save -o ${ISTIO_DOCKER_TAR}/$(subst docker.,,$(TGT)).tar $(HUB)/$(subst docker.,,$(TGT)):$(TAG) && \
         gzip ${ISTIO_DOCKER_TAR}/$(subst docker.,,$(TGT)).tar)))

# create a DOCKER_TAR_TARGETS that's each of DOCKER_TARGETS with a tar. prefix
DOCKER_TAR_TARGETS:=
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

# This target pushes each docker image to specified HUB and TAG.
# The push scripts support a comma-separated list of HUB(s) and TAG(s),
# but I'm not sure this is worth the added complexity to support.

# Deprecated - just use docker, no need to retag.
docker.tag: docker

# Will build and push docker images.
docker.push: $(DOCKER_PUSH_TARGETS)

# Base image for 'debug' containers.
# You can run it first to use local changes (or guarantee it is built from scratch)
docker.basedebug:
	docker build -t istionightly/base_debug -f docker/Dockerfile.xenial_debug docker/

# Job run from the nightly cron to publish an up-to-date xenial with the debug tools.
docker.push.basedebug: docker.basedebug
	docker push istionightly/base_debug:latest
