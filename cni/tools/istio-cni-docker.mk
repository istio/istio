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

.PHONY: cni.docker cni.docker.save

# Docker target will build the go binaries and package the docker for local testing.
# It does not upload to a registry.
cni.docker: build cni.docker.all

.SECONDEXPANSION: #allow $@ to be used in dependency list

# directives to copy files to docker scratch directory

# tell make which files are copied from go/out
CNI_DOCKER_FILES_FROM_ISTIO_OUT:=istio-cni istio-cni-repair

$(foreach FILE,$(CNI_DOCKER_FILES_FROM_ISTIO_OUT), \
        $(eval $(ISTIO_DOCKER)/$(FILE): $(ISTIO_OUT)/$(FILE) | $(ISTIO_DOCKER); cp $$< $$(@D)))

# tell make which files are copied from the source tree
CNI_DOCKER_FILES_FROM_SOURCE:=tools/packaging/common/istio-iptables.sh
$(foreach FILE,$(CNI_DOCKER_FILES_FROM_SOURCE), \
        $(eval $(ISTIO_DOCKER)/$(notdir $(FILE)): $(FILE) | $(ISTIO_DOCKER); cp $(FILE) $$(@D)))

cni.docker.install-cni: $(ISTIO_OUT)/istio-cni $(ISTIO_OUT)/istio-cni-repair \
    	cni/tools/packaging/common/istio-iptables.sh \
		cni/deployments/kubernetes/install/scripts/install-cni.sh \
		cni/deployments/kubernetes/install/scripts/istio-cni.conf.default \
		cni/deployments/kubernetes/Dockerfile.install-cni \
		cni/deployments/kubernetes/install/scripts/filter.jq
	mkdir -p $(ISTIO_DOCKER)/install-cni
	cp $^ $(ISTIO_DOCKER)/install-cni
	time docker build -t $(HUB)/install-cni:$(TAG) \
		-f $(ISTIO_DOCKER)/install-cni/Dockerfile.install-cni \
		$(ISTIO_DOCKER)/install-cni

CNI_DOCKER_TARGETS:=cni.docker.install-cni

# create a DOCKER_PUSH_TARGETS that's each of DOCKER_TARGETS with a push. prefix
CNI_DOCKER_PUSH_TARGETS:=
$(foreach TGT,$(CNI_DOCKER_TARGETS),$(eval CNI_DOCKER_PUSH_TARGETS+=push.$(TGT)))

# for each docker.XXX target create a push.docker.XXX target that pushes
# the local docker image to another hub
# a possible optimization is to use tag.$(TGT) as a dependency to do the tag for us
$(foreach TGT,$(CNI_DOCKER_TARGETS),$(eval push.$(TGT): | $(TGT) ; \
        time (docker push $(HUB)/$(subst docker.,,$(TGT)):$(TAG))))

# create a DOCKER_PUSH_TARGETS that's each of DOCKER_TARGETS with a push. prefix
CNI_DOCKER_PUSH_TARGETS:=
$(foreach TGT,$(CNI_DOCKER_TARGETS),$(eval CNI_DOCKER_PUSH_TARGETS+=push.$(TGT)))

# Will build and push docker images.
cni.docker.push: $(CNI_DOCKER_PUSH_TARGETS)

# This target will package all docker images used in test and release, without re-building
# go binaries. It is intended for CI/CD systems where the build is done in separate job.
cni.docker.all: $(CNI_DOCKER_TARGETS)

# create a DOCKER_TAR_TARGETS that's each of DOCKER_TARGETS with a tar. prefix
CNI_DOCKER_TAR_TARGETS:=
$(foreach TGT,$(CNI_DOCKER_TARGETS),$(eval tar.$(TGT): $(TGT) | $(ISTIO_DOCKER_TAR) ; \
   time (docker save -o ${ISTIO_DOCKER_TAR}/$(subst docker.,,$(TGT)).tar $(HUB)/$(subst docker.,,$(TGT)):$(TAG) && \
         gzip ${ISTIO_DOCKER_TAR}/$(subst docker.,,$(TGT)).tar)))

# create a DOCKER_TAR_TARGETS that's each of DOCKER_TARGETS with a tar. prefix DOCKER_TAR_TARGETS:=
$(foreach TGT,$(CNI_DOCKER_TARGETS),$(eval CNI_DOCKER_TAR_TARGETS+=tar.$(TGT)))

# this target saves a tar.gz of each docker image to ${ISTIO_OUT}/docker/
cni.docker.save: $(CNI_DOCKER_TAR_TARGETS)
