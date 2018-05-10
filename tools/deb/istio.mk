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
		/var/lib/istio/envoy/sidecar.env

$(foreach DEST,$(ISTIO_DEB_DEST),\
        $(eval ${ISTIO_OUT}/istio-sidecar.deb:   tools/deb/$(notdir $(DEST))) \
        $(eval SIDECAR_FILES+=src/istio.io/istio/tools/deb/$(notdir $(DEST))=$(DEST)))

SIDECAR_FILES+=src/istio.io/istio/tools/deb/envoy_bootstrap_v2.json=/var/lib/istio/envoy/envoy_bootstrap_tmpl.json

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
		--depends iproute2 \
		--depends iptables \
		$(SIDECAR_FILES)

# Install the deb in a docker image, for testing of the install process.
deb/docker: hyperistio
	mkdir -p ${OUT_DIR}/deb
	cp tools/deb/Dockerfile ${OUT_DIR}/deb
	cp tests/testdata/config/*.yaml ${OUT_DIR}/deb
	cp -a tests/testdata/certs ${OUT_DIR}/deb
	cp ${ISTIO_OUT}/hyperistio ${OUT_DIR}/deb
	cp ${ISTIO_OUT}/istio-sidecar.deb ${OUT_DIR}/deb/istio.deb
	docker build -t istio_deb -f ${OUT_DIR}/deb/Dockerfile ${OUT_DIR}/deb/

deb/test:
	docker run --cap-add=NET_ADMIN --rm -v ${ISTIO_GO}/tools/deb/deb_test.sh:/tmp/deb_test.sh istio_deb /tmp/deb_test.sh
	#docker run --cap-add=NET_ADMIN --rm -v ${ISTIO_GO}/tools/deb/deb_test_auth.sh:/tmp/deb_test.sh istio_deb /tmp/deb_test.sh

# Run the docker image including the installed debian, with access to all source
# code.
deb/docker-run:
	docker run --cap-add=NET_ADMIN --rm \
	  -v ${GO_TOP}:${GO_TOP} \
      -w ${PWD} \
      --add-host echo:10.1.1.1 \
      --add-host istio-pilot.istio-system:127.0.0.1 \
      -p 127.0.0.1:16001:8080 \
      -p 127.0.0.1:16002:7070 \
      -p 127.0.0.1:16003:7072 \
      -p 127.0.0.1:16004:7073 \
      -p 127.0.0.1:16005:7074 \
      -p 127.0.0.1:16006:7075 \
      -e GOPATH=${GOPATH} \
      -it istio_deb /bin/bash

.PHONY: \
	deb \
	deb/build-in-docker \
	deb/docker \
	deb/docker-run \
	deb/fpm \
	deb/test \
	sidecar.deb
