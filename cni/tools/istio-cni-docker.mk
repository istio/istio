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

.PHONY: docker docker.save

# Docker target will build the go binaries and package the docker for local testing.
# It does not upload to a registry.
docker: build docker.all

$(ISTIO_DOCKER) $(ISTIO_DOCKER_TAR):
	mkdir -p $@

.SECONDEXPANSION: #allow $@ to be used in dependency list

# directives to copy files to docker scratch directory

# tell make which files are copied from go/out
DOCKER_FILES_FROM_ISTIO_OUT:=istio-cni istio-cni-repair

$(foreach FILE,$(DOCKER_FILES_FROM_ISTIO_OUT), \
        $(eval $(ISTIO_DOCKER)/$(FILE): $(ISTIO_OUT)/$(FILE) | $(ISTIO_DOCKER); cp $$< $$(@D)))

# tell make which files are copied from the source tree
DOCKER_FILES_FROM_SOURCE:=tools/packaging/common/istio-iptables.sh
$(foreach FILE,$(DOCKER_FILES_FROM_SOURCE), \
        $(eval $(ISTIO_DOCKER)/$(notdir $(FILE)): $(FILE) | $(ISTIO_DOCKER); cp $(FILE) $$(@D)))

docker.install-cni: $(ISTIO_OUT)/istio-cni $(ISTIO_OUT)/istio-cni-repair \
    tools/packaging/common/istio-iptables.sh \
		deployments/kubernetes/install/scripts/install-cni.sh \
		deployments/kubernetes/install/scripts/istio-cni.conf.default \
		deployments/kubernetes/Dockerfile.install-cni \
		deployments/kubernetes/install/scripts/filter.jq
	mkdir -p $(ISTIO_DOCKER)/install-cni
	cp $^ $(ISTIO_DOCKER)/install-cni
	time docker build -t $(HUB)/install-cni:$(TAG) \
		-f $(ISTIO_DOCKER)/install-cni/Dockerfile.install-cni \
		$(ISTIO_DOCKER)/install-cni

DOCKER_TARGETS:=docker.install-cni

# create a DOCKER_PUSH_TARGETS that's each of DOCKER_TARGETS with a push. prefix
DOCKER_PUSH_TARGETS:=
$(foreach TGT,$(DOCKER_TARGETS),$(eval DOCKER_PUSH_TARGETS+=push.$(TGT)))

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

# This target will package all docker images used in test and release, without re-building
# go binaries. It is intended for CI/CD systems where the build is done in separate job.
docker.all: $(DOCKER_TARGETS)

# create a DOCKER_TAR_TARGETS that's each of DOCKER_TARGETS with a tar. prefix
DOCKER_TAR_TARGETS:=
$(foreach TGT,$(DOCKER_TARGETS),$(eval tar.$(TGT): $(TGT) | $(ISTIO_DOCKER_TAR) ; \
   time (docker save -o ${ISTIO_DOCKER_TAR}/$(subst docker.,,$(TGT)).tar $(HUB)/$(subst docker.,,$(TGT)):$(TAG) && \
         gzip ${ISTIO_DOCKER_TAR}/$(subst docker.,,$(TGT)).tar)))

# create a DOCKER_TAR_TARGETS that's each of DOCKER_TARGETS with a tar. prefix DOCKER_TAR_TARGETS:=
$(foreach TGT,$(DOCKER_TARGETS),$(eval DOCKER_TAR_TARGETS+=tar.$(TGT)))

# this target saves a tar.gz of each docker image to ${ISTIO_OUT}/docker/
docker.save: $(DOCKER_TAR_TARGETS)
