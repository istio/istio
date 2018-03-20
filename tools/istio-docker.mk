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

$(ISTIO_DOCKER_TAR):
	mkdir -p $@

.SECONDEXPANSION: #allow $@ to be used in dependency list

DOCKER_OPT.proxy:=--build-arg ENVOY_NAME=$(notdir $(ISTIO_ENVOY_RELEASE_PATH))
DOCKER_OPT.proxy_debug:=--build-arg ENVOY_DEBUG_NAME=$(notdir $(ISTIO_ENVOY_DEBUG_PATH))

# generated content. These are generated in ISTIO_OUT rather than ISTIO_DOCKER to help
# keep the crt and key files the same across images even if/when ISTIO_DOCKER is deleted.
$(ISTIO_OUT)/istio_ca.crt $(ISTIO_OUT)/istio_ca.key: ${GEN_CERT} | ${ISTIO_OUT}
	${GEN_CERT} --key-size=2048 --out-cert=${ISTIO_OUT}/istio_ca.crt \
                    --out-priv=${ISTIO_OUT}/istio_ca.key --organization="k8s.cluster.local" \
                    --self-signed=true --ca=true
$(ISTIO_OUT)/node_agent.crt $(ISTIO_OUT)/node_agent.key: ${GEN_CERT} $(ISTIO_OUT)/istio_ca.crt $(ISTIO_OUT)/istio_ca.key
	${GEN_CERT} --key-size=2048 --out-cert=${ISTIO_OUT}/node_agent.crt \
                    --out-priv=${ISTIO_OUT}/node_agent.key --organization="NodeAgent" \
                    --host="nodeagent.google.com" --signer-cert=${ISTIO_OUT}/istio_ca.crt \
                    --signer-priv=${ISTIO_OUT}/istio_ca.key

# pilot docker images

# ifeq ($(DEBUG_IMAGE),1)
# cp $(ISTIO_DOCKER)/pilotapp/Dockerfile.app $(ISTIO_DOCKER)/pilotapp/Dockerfile.appdbg
# sed -e "s,FROM scratch,FROM $(HUB)/proxy_debug:$(TAG)," $(ISTIO_DOCKER)/pilotapp/Dockerfile.appdbg > $(ISTIO_DOCKER)/pilotapp/Dockerfile.appd
# endif
docker.app: $(ISTIO_OUT)/pilot-test-client $(ISTIO_OUT)/pilot-test-server \
            pilot/docker/certs/cert.crt pilot/docker/certs/cert.key
docker.eurekamirror: $(ISTIO_OUT)/pilot-test-eurekamirror
# ifeq ($(DEBUG_IMAGE),1)
# use Dockerfile.proxy_debug rather than Dockerfile.proxy for "proxy" image
# endif
docker.pilot:        $(ISTIO_OUT)/pilot-discovery
docker.proxy docker.proxy_debug: $(ISTIO_OUT)/pilot-agent
docker.proxy: ${ISTIO_ENVOY_RELEASE_PATH}
docker.proxy_debug: ${ISTIO_ENVOY_DEBUG_PATH}
docker.proxy docker.proxy_debug: pilot/docker/envoy_pilot.json pilot/docker/envoy_pilot_auth.json \
                  pilot/docker/envoy_mixer.json pilot/docker/envoy_mixer_auth.json
docker.proxy docker.proxy_debug: tools/deb/envoy_bootstrap_tmpl.json
docker.proxy_init: pilot/docker/prepare_proxy.sh
docker.sidecar_injector: $(ISTIO_OUT)/sidecar-injector

PILOT_DOCKER:=docker.app docker.eurekamirror docker.pilot docker.proxy \
              docker.proxy_debug docker.proxy_init docker.sidecar_injector
$(PILOT_DOCKER): pilot/docker/Dockerfile$$(suffix $$@)
	$(DOCKER_RULE)

# addons docker images

SERVICEGRAPH_DOCKER:=docker.servicegraph docker.servicegraph_debug
$(SERVICEGRAPH_DOCKER): addons/servicegraph/docker/Dockerfile$$(suffix $$@) \
		$(ISTIO_OUT)/servicegraph addons/servicegraph/js addons/servicegraph/force
	$(DOCKER_RULE)

# mixer docker images

MIXER_DOCKER:=docker.mixer docker.mixer_debug
$(MIXER_DOCKER): mixer/docker/Dockerfile$$(suffix $$@) \
		 docker/ca-certificates.tgz $(ISTIO_OUT)/mixs
	$(DOCKER_RULE)

# security docker images

docker.istio-ca:        $(ISTIO_OUT)/istio_ca docker/ca-certificates.tgz
docker.istio-ca-test:   $(ISTIO_OUT)/istio_ca $(ISTIO_OUT)/istio_ca.crt $(ISTIO_OUT)/istio_ca.key
docker.node-agent:      $(ISTIO_OUT)/node_agent
docker.node-agent-test: $(ISTIO_OUT)/node_agent $(ISTIO_OUT)/istio_ca.crt \
                        $(ISTIO_OUT)/node_agent.crt $(ISTIO_OUT)/node_agent.key \
                        security/docker/start_app.sh security/docker/app.js
docker.multicluster-ca: $(ISTIO_OUT)/multicluster_ca
docker.flexvolumedriver: $(ISTIO_OUT)/flexvolume security/docker/start_driver.sh

SECURITY_DOCKER:=docker.istio-ca docker.istio-ca-test docker.node-agent docker.node-agent-test docker.multicluster-ca docker.flexvolumedriver
$(SECURITY_DOCKER): security/docker/Dockerfile$$(suffix $$@)
	$(DOCKER_RULE)

# grafana image

docker.grafana: addons/grafana/Dockerfile$$(suffix $$@) addons/grafana/dashboards.yaml \
                addons/grafana/datasources.yaml addons/grafana/grafana.ini addons/grafana/dashboards
	$(DOCKER_RULE)

DOCKER_TARGETS:=$(PILOT_DOCKER) $(SERVICEGRAPH_DOCKER) $(MIXER_DOCKER) $(SECURITY_DOCKER) docker.grafana

DOCKER_RULE=time (mkdir -p $(ISTIO_DOCKER)/$@ && cp -r $^ $(ISTIO_DOCKER)/$@/ && cd $(ISTIO_DOCKER)/$@/ && \
            docker build $(DOCKER_OPT$(suffix $@)) -t $(HUB)/$(subst docker.,,$@):$(TAG) -f Dockerfile$(suffix $@) .) && \
            cd .. && rm -rf $(ISTIO_DOCKER)/$@

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

# if first part of URL (i.e., hostname) is gcr.io then use gcloud for push
$(if $(findstring gcr.io,$(firstword $(subst /, ,$(HUB)))),\
        $(eval DOCKER_PUSH_CMD:=gcloud docker -- push),$(eval DOCKER_PUSH_CMD:=docker push))

# for each docker.XXX target create a push.docker.XXX target that pushes
# the local docker image to another hub
# a possible optimization is to use tag.$(TGT) as a dependency to do the tag for us
$(foreach TGT,$(DOCKER_TARGETS),$(eval push.$(TGT): | $(TGT) ; \
        time ($(DOCKER_PUSH_CMD) $(HUB)/$(subst docker.,,$(TGT)):$(TAG))))

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
