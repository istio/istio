.PHONY: deb/build-in-docker sidecar.deb deb

# Make the deb image using the CI/CD image and docker, for users who don't have 'fpm' installed.
# TODO: use 'which fpm' to detect if fpm is installed on host, consolidate under one target ('deb')
deb/build-in-docker:
	(cd ${TOP}; docker run --rm -u $(shell id -u) -it \
        -v ${GO_TOP}:${GO_TOP} \
        -w ${PWD} \
        -e USER=${USER} \
        -e GOPATH=${GOPATH} \
		--entrypoint /bin/bash ${CI_HUB}/ci:${CI_VERSION} \
		-c "make deb/fpm")

# Create the 'sidecar' deb, including envoy and istio agents and configs.
# This target uses a locally installed 'fpm' - use 'docker.sidecar.deb' to use
# the builder image.
# TODO: consistent layout, possibly /opt/istio-VER/...
sidecar.deb: ${ISTIO_OUT}/istio-sidecar.deb

deb: ${ISTIO_OUT}/istio-sidecar.deb

# Base directory for istio binaries. Likely to change !
ISTIO_DEB_BIN=/usr/local/bin

ISTIO_DEB_DEPS:=envoy pilot-agent pilot-discovery node_agent istioctl mixs istio_ca
SIDECAR_FILES:=
# subst is used to turn an absolute path into the relative path that fpm seems to expect
$(foreach DEP,$(ISTIO_DEB_DEPS),\
        $(eval ${ISTIO_OUT}/istio-sidecar.deb: $(ISTIO_OUT)/$(DEP)) \
        $(eval SIDECAR_FILES+=$(subst $(GO_TOP)/,,$(ISTIO_OUT))/$(DEP)=$(ISTIO_DEB_BIN)/$(DEP)) )

ISTIO_DEB_DEST:=${ISTIO_DEB_BIN}/istio-start.sh \
		${ISTIO_DEB_BIN}/istio-iptables.sh \
		/lib/systemd/system/istio.service \
		/lib/systemd/system/istio-auth-node-agent.service \
		/var/lib/istio/envoy/sidecar.env \
		/var/lib/istio/envoy/envoy_bootstrap_tmpl.json

$(foreach DEST,$(ISTIO_DEB_DEST),\
        $(eval ${ISTIO_OUT}/istio-sidecar.deb:   tools/deb/$(notdir $(DEST))) \
        $(eval SIDECAR_FILES+=src/istio.io/istio/tools/deb/$(notdir $(DEST))=$(DEST)))

# original name used in 0.2 - will be updated to 'istio.deb' since it now includes all istio binaries.
ISTIO_DEB_NAME ?= istio-sidecar

# TODO: rename istio-sidecar.deb to istio.deb

# Note: adding --deb-systemd ${GO_TOP}/src/istio.io/istio/tools/deb/istio.service will result in
# a /etc/systemd/system/multi-user.target.wants/istio.service and auto-start. Currently not used
# since we need configuration.
# --iteration 1 adds a "-1" suffix to the version that didn't exist before
${ISTIO_OUT}/istio-sidecar.deb: | ${ISTIO_OUT}
	$(MAKE) deb/fpm

# This got way too complex - used only to run fpm in a container.
deb/fpm:
	rm -f ${ISTIO_OUT}/istio-sidecar.deb
	fpm -s dir -t deb -n ${ISTIO_DEB_NAME} -p ${ISTIO_OUT}/istio-sidecar.deb --version ${VERSION} -C ${GO_TOP} -f \
		--url http://istio.io  \
		--license Apache \
		--vendor istio.io \
		--maintainer istio@istio.io \
		--after-install tools/deb/postinst.sh \
		--config-files /var/lib/istio/envoy/envoy_bootstrap_tmpl.json \
		--config-files /var/lib/istio/envoy/sidecar.env \
		--description "Istio" \
		$(SIDECAR_FILES)

.PHONY: deb/docker

# Install the deb in a docker image, for testing.
deb/docker:
	mkdir -p ${ISTIO_OUT}/deb
	cp tools/deb/Dockerfile ${ISTIO_OUT}/deb
	cp ${ISTIO_OUT}/istio-sidecar.deb ${ISTIO_OUT}/deb/istio.deb
	docker build -t istio_deb -f ${ISTIO_OUT}/deb/Dockerfile ${ISTIO_OUT}/deb/

deb/test: deb-docker tools/deb/deb_test.sh
	docker run --cap-add=NET_ADMIN --rm -v ${ISTIO_GO}/tools/deb/deb_test.sh:/tmp/deb_test.sh istio_deb /tmp/deb_test.sh

deb/docker-run: deb-docker  tools/deb/deb_test.sh
	docker run --cap-add=NET_ADMIN --rm -v ${ISTIO_GO}/tools/deb/deb_test.sh:/tmp/deb_test.sh -it istio_deb /bin/bash
