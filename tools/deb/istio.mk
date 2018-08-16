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

ISTIO_DEB_DEPS:=pilot-discovery istioctl mixs istio_ca
ISTIO_FILES:=
# subst is used to turn an absolute path into the relative path that fpm seems to expect
$(foreach DEP,$(ISTIO_DEB_DEPS),\
        $(eval ${ISTIO_OUT}/istio.deb: $(ISTIO_OUT)/$(DEP)) \
        $(eval ISTIO_FILES+=$(subst $(GO_TOP)/,,$(ISTIO_OUT))/$(DEP)=$(ISTIO_DEB_BIN)/$(DEP)) )

SIDECAR_DEB_DEPS:=envoy pilot-agent node_agent
SIDECAR_FILES:=
# subst is used to turn an absolute path into the relative path that fpm seems to expect
$(foreach DEP,$(SIDECAR_DEB_DEPS),\
        $(eval ${ISTIO_OUT}/istio-sidecar.deb: $(ISTIO_OUT)/$(DEP)) \
        $(eval SIDECAR_FILES+=$(subst $(GO_TOP)/,,$(ISTIO_OUT))/$(DEP)=$(ISTIO_DEB_BIN)/$(DEP)) )

ISTIO_DEB_DEST:=${ISTIO_DEB_BIN}/istio-start.sh \
		${ISTIO_DEB_BIN}/istio-node-agent-start.sh \
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

#remove leading charecters since debian version expects to start with digit
DEB_VERSION := $(shell echo $(VERSION) | sed 's/^[a-z]*-//')

# Package the sidecar deb file.
deb/fpm:
	rm -f ${ISTIO_OUT}/istio-sidecar.deb
	fpm -s dir -t deb -n ${ISTIO_DEB_NAME} -p ${ISTIO_OUT}/istio-sidecar.deb --version $(DEB_VERSION) -C ${GO_TOP} -f \
		--url http://istio.io  \
		--license Apache \
		--vendor istio.io \
		--maintainer istio@istio.io \
		--after-install tools/deb/postinst.sh \
		--config-files /var/lib/istio/envoy/envoy_bootstrap_tmpl.json \
		--config-files /var/lib/istio/envoy/sidecar.env \
		--description "Istio Sidecar" \
		--depends iproute2 \
		--depends iptables \
		$(SIDECAR_FILES)

${ISTIO_OUT}/istio.deb:
	rm -f ${ISTIO_OUT}/istio.deb
	fpm -s dir -t deb -n istio -p ${ISTIO_OUT}/istio.deb --version $(DEB_VERSION) -C ${GO_TOP} -f \
		--url http://istio.io  \
		--license Apache \
		--vendor istio.io \
		--maintainer istio@istio.io \
		--description "Istio" \
		$(ISTIO_FILES)

# Install the deb in a docker image, for testing of the install process.
deb/docker: hyperistio build deb/fpm ${ISTIO_OUT}/istio.deb
	mkdir -p ${OUT_DIR}/deb
	cp tools/deb/Dockerfile tools/deb/deb_test.sh ${OUT_DIR}/deb
	cp tests/testdata/config/*.yaml ${OUT_DIR}/deb
	cp -a tests/testdata/certs ${OUT_DIR}/deb
	cp ${ISTIO_OUT}/hyperistio ${OUT_DIR}/deb
	cp ${GOPATH}/bin/{kube-apiserver,etcd,kubectl} ${OUT_DIR}/deb
	cp ${ISTIO_OUT}/istio-sidecar.deb ${OUT_DIR}/deb/istio-sidecar.deb
	cp ${ISTIO_OUT}/istio.deb ${OUT_DIR}/deb/istio.deb
	docker build -t istio_deb -f ${OUT_DIR}/deb/Dockerfile ${OUT_DIR}/deb/

deb/test:
	docker run --cap-add=NET_ADMIN --rm -v ${ISTIO_GO}/tools/deb/deb_test.sh:/tmp/deb_test.sh istio_deb /tmp/deb_test.sh

# For the test, by default use a local pilot.
# Set it to 172.18.0.1 to run against a pilot or hyperistio running in IDE.
# You may need to enable 15007 in the local machine firewall for this to work.
DEB_PILOT_IP ?= 127.0.0.1
DEB_CMD ?= /bin/bash
DEB_IP ?= 172.18.0.3
DEB_PORT_PREFIX ?= 1600

# TODO: docker compose ?

# Run the docker image including the installed debian, with access to all source
# code. Useful for debugging/experiments with iptables.
#
# Before running:
# docker network create --subnet=172.18.0.0/16 istiotest
# The IP of the docker matches the byon-docker service entry
deb/run/docker:
	docker run --cap-add=NET_ADMIN --rm \
	  -v ${GO_TOP}:${GO_TOP} \
      -w ${PWD} \
      --net istiotest --ip ${DEB_IP} \
      --add-host echo:10.1.1.1 \
      --add-host byon.test.istio.io:10.1.1.2 \
      --add-host byon-docker.test.istio.io:10.1.1.2 \
      --add-host istio-pilot.istio-system:${DEB_PILOT_IP} \
      ${DEB_ENV} -e ISTIO_SERVICE_CIDR=10.1.1.0/24 \
      -e ISTIO_INBOUND_PORTS=7070,7072,7073,7074,7075 \
      -e PILOT_CERT_DIR=/var/lib/istio/pilot \
      -p 127.0.0.1:${DEB_PORT_PREFIX}1:15007 \
      -p 127.0.0.1:${DEB_PORT_PREFIX}2:7070 \
      -p 127.0.0.1:${DEB_PORT_PREFIX}3:7072 \
      -p 127.0.0.1:${DEB_PORT_PREFIX}4:7073 \
      -p 127.0.0.1:${DEB_PORT_PREFIX}5:7074 \
      -p 127.0.0.1:${DEB_PORT_PREFIX}6:7075 \
      -p 127.0.0.1:${DEB_PORT_PREFIX}7:15011 \
      -p 127.0.0.1:${DEB_PORT_PREFIX}8:15010 \
      -e GOPATH=${GOPATH} \
      -it istio_deb ${DEB_CMD}

deb/run/debug:
	$(MAKE) deb/run/docker DEB_ENV="-e DEB_PILOT_IP=172.18.0.1"

deb/run/tproxy:
	$(MAKE) deb/run/docker DEB_PORT_PREFIX=1610 DEB_IP=172.18.0.4 DEB_ENV="-e ISTIO_INBOUND_INTERCEPTION_MODE=TPROXY"

deb/run/mtls:
	$(MAKE) deb/run/docker DEB_PORT_PREFIX=1620 -e DEB_PILOT_IP=172.18.0.1 DEB_IP=172.18.0.5 DEB_ENV="-e ISTIO_PILOT_PORT=15005 -e ISTIO_CP_AUTH=MUTUAL_TLS"

# Similar with above, but using a pilot running on the local machine
deb/run/docker-debug:
	$(MAKE) deb/run/docker PILOT_IP=

#
deb/docker-run: deb/docker deb/run/docker

.PHONY: \
	deb \
	deb/build-in-docker \
	deb/docker \
	deb/docker-run \
	deb/run/docker \
	deb/fpm \
	deb/test \
	sidecar.deb
