#remove leading characters since package version expects to start with digit
PACKAGE_VERSION ?= $(shell echo $(VERSION) | sed 's/^[a-z]*-//' | sed 's/-//')

# Creates the 2 packages. BUILD_WITH_CONTAINER=1 or in CI/CD (BUILD_WITH_CONTAINER=0)
#
# Development/manual testing:
#    make deb          - builds debian packaging
#    make deb/docker   - builds a test docker image

deb: ${TARGET_OUT_LINUX}/release/istio-sidecar.deb ${TARGET_OUT_LINUX}/release/istio.deb

# fpm likes to add extremely high levels of compression. This is fine for release, but for local runs
# where we are just pushing to a local registry (compressed again!), it adds ~1min to builds.
ifneq ($(FAST_VM_BUILDS),)
# TODO(debian): https://github.com/jordansissel/fpm/pull/1879
DEB_COMPRESSION=
RPM_COMPRESSION=--rpm-compression=none
endif

# Base directory for istio binaries. Likely to change !
ISTIO_DEB_BIN=/usr/local/bin

# Home directory of istio-proxy user. It is symlinked /etc/istio --> /var/lib/istio
ISTIO_PROXY_HOME=/var/lib/istio

ISTIO_DEB_DEPS:=pilot-discovery istioctl
ISTIO_FILES:=
$(foreach DEP,$(ISTIO_DEB_DEPS),\
        $(eval ${TARGET_OUT_LINUX}/release/istio.deb: $(TARGET_OUT_LINUX)/$(DEP)) \
        $(eval ISTIO_FILES+=$(TARGET_OUT_LINUX)/$(DEP)=$(ISTIO_DEB_BIN)/$(DEP)) )

SIDECAR_DEB_DEPS:=envoy pilot-agent
SIDECAR_FILES:=
$(foreach DEP,$(SIDECAR_DEB_DEPS),\
        $(eval ${TARGET_OUT_LINUX}/release/istio-sidecar.deb: $(TARGET_OUT_LINUX)/$(DEP)) \
        $(eval ${TARGET_OUT_LINUX}/release/istio-sidecar.rpm: $(TARGET_OUT_LINUX)/$(DEP)) \
        $(eval SIDECAR_FILES+=$(TARGET_OUT_LINUX)/$(DEP)=$(ISTIO_DEB_BIN)/$(DEP)) )

${TARGET_OUT_LINUX}/release/istio-sidecar-centos-7.rpm: $(TARGET_OUT_LINUX)/envoy-centos
${TARGET_OUT_LINUX}/release/istio-sidecar-centos-7.rpm: $(TARGET_OUT_LINUX)/pilot-agent
SIDECAR_CENTOS_7_FILES:=$(TARGET_OUT_LINUX)/envoy-centos=$(ISTIO_DEB_BIN)/envoy
SIDECAR_CENTOS_7_FILES+=$(TARGET_OUT_LINUX)/pilot-agent=$(ISTIO_DEB_BIN)/pilot-agent

ISTIO_DEB_DEST:=${ISTIO_DEB_BIN}/istio-start.sh \
		/lib/systemd/system/istio.service \
		/var/lib/istio/envoy/sidecar.env

$(foreach DEST,$(ISTIO_DEB_DEST),\
        $(eval ${TARGET_OUT_LINUX}/istio-sidecar.deb:   tools/packaging/common/$(notdir $(DEST))) \
        $(eval SIDECAR_FILES+=${REPO_ROOT}/tools/packaging/common/$(notdir $(DEST))=$(DEST)) \
        $(eval SIDECAR_CENTOS_7_FILES+=${REPO_ROOT}/tools/packaging/common/$(notdir $(DEST))=$(DEST)))

SIDECAR_FILES+=${REPO_ROOT}/tools/packaging/common/envoy_bootstrap.json=/var/lib/istio/envoy/envoy_bootstrap_tmpl.json
SIDECAR_CENTOS_7_FILES+=${REPO_ROOT}/tools/packaging/common/envoy_bootstrap.json=/var/lib/istio/envoy/envoy_bootstrap_tmpl.json

ISTIO_EXTENSIONS:=stats-filter.wasm \
                  stats-filter.compiled.wasm \
                  metadata-exchange-filter.wasm \
                  metadata-exchange-filter.compiled.wasm

$(foreach EXT,$(ISTIO_EXTENSIONS),\
        $(eval SIDECAR_FILES+=${ISTIO_ENVOY_LINUX_RELEASE_DIR}/$(EXT)=$(ISTIO_PROXY_HOME)/extensions/$(EXT)) \
        $(eval SIDECAR_CENTOS_7_FILES+=${ISTIO_ENVOY_LINUX_RELEASE_DIR}/$(EXT)=$(ISTIO_PROXY_HOME)/extensions/$(EXT)))

# original name used in 0.2 - will be updated to 'istio.deb' since it now includes all istio binaries.
SIDECAR_PACKAGE_NAME ?= istio-sidecar

# TODO: rename istio-sidecar.deb to istio.deb

# Note: adding --deb-systemd ${REPO_ROOT}/tools/packaging/common/istio.service will result in
# a /etc/systemd/system/multi-user.target.wants/istio.service and auto-start. Currently not used
# since we need configuration.
# --iteration 1 adds a "-1" suffix to the version that didn't exist before
${TARGET_OUT_LINUX}/release/istio-sidecar.deb: | ${TARGET_OUT_LINUX} deb/fpm
${TARGET_OUT_LINUX}/release/istio-sidecar.rpm: | ${TARGET_OUT_LINUX} rpm/fpm
${TARGET_OUT_LINUX}/release/istio-sidecar-centos-7.rpm: | ${TARGET_OUT_LINUX} rpm-7/fpm

# Package the sidecar rpm file.
rpm/fpm:
	rm -f ${TARGET_OUT_LINUX}/release/istio-sidecar.rpm
	fpm -s dir -t rpm -n ${SIDECAR_PACKAGE_NAME} -p ${TARGET_OUT_LINUX}/release/istio-sidecar.rpm --version $(PACKAGE_VERSION) -f \
		--url http://istio.io  \
		--license Apache \
		--vendor istio.io \
		--maintainer istio@istio.io \
		--after-install tools/packaging/postinst.sh \
		--config-files /var/lib/istio/envoy/envoy_bootstrap_tmpl.json \
		--config-files /var/lib/istio/envoy/sidecar.env \
		--description "Istio Sidecar" \
		--depends iproute \
		--depends iptables \
		--depends sudo \
		$(RPM_COMPRESSION) \
		$(SIDECAR_FILES)

# Centos 7 compatible RPM
rpm-7/fpm:
	rm -f ${TARGET_OUT_LINUX}/release/istio-sidecar-centos-7.rpm
	fpm -s dir -t rpm -n ${SIDECAR_PACKAGE_NAME} -p ${TARGET_OUT_LINUX}/release/istio-sidecar-centos-7.rpm --version $(PACKAGE_VERSION) -f \
		--url http://istio.io  \
		--license Apache \
		--vendor istio.io \
		--maintainer istio@istio.io \
		--after-install tools/packaging/postinst.sh \
		--config-files /var/lib/istio/envoy/envoy_bootstrap_tmpl.json \
		--config-files /var/lib/istio/envoy/sidecar.env \
		--description "Istio Sidecar" \
		--depends iproute \
		--depends iptables \
		--depends sudo \
		$(RPM_COMPRESSION) \
		$(SIDECAR_CENTOS_7_FILES)

# Package the sidecar deb file.
deb/fpm:
	rm -f ${TARGET_OUT_LINUX}/release/istio-sidecar.deb
	fpm -s dir -t deb -n ${SIDECAR_PACKAGE_NAME} -p ${TARGET_OUT_LINUX}/release/istio-sidecar.deb --version $(PACKAGE_VERSION) -f \
		--url http://istio.io  \
		--license Apache \
		--vendor istio.io \
		--maintainer istio@istio.io \
		--after-install tools/packaging/postinst.sh \
		--config-files /var/lib/istio/envoy/envoy_bootstrap_tmpl.json \
		--config-files /var/lib/istio/envoy/sidecar.env \
		--description "Istio Sidecar" \
		--depends iproute2 \
		--depends iptables \
		--depends sudo \
		$(DEB_COMPRESSION) \
		$(SIDECAR_FILES)

${TARGET_OUT_LINUX}/release/istio.deb:
	rm -f ${TARGET_OUT_LINUX}/release/istio.deb
	fpm -s dir -t deb -n istio -p ${TARGET_OUT_LINUX}/release/istio.deb --version $(PACKAGE_VERSION) -f \
		--url http://istio.io  \
		--license Apache \
		--vendor istio.io \
		--maintainer istio@istio.io \
		--description "Istio" \
		$(ISTIO_FILES)

# TODO: use k8s style - /etc/pki/istio/...
PKI_DIR ?= tests/testdata/certs/cacerts
VM_PKI_DIR ?= tests/testdata/certs/vm

testcert-gen: ${GEN_CERT}
	mkdir -p ${PKI_DIR}
	mkdir -p ${VM_PKI_DIR}
	${GEN_CERT} -ca  --out-priv ${PKI_DIR}/ca-key.pem --out-cert ${PKI_DIR}/ca-cert.pem  -organization "istio ca"
	cp ${PKI_DIR}/ca-cert.pem ${PKI_DIR}/root-cert.pem
	cp ${PKI_DIR}/ca-cert.pem ${PKI_DIR}/cert-chain.pem
	${GEN_CERT} -signer-cert ${PKI_DIR}/ca-cert.pem -signer-priv ${PKI_DIR}/ca-key.pem \
	 	-out-cert ${VM_PKI_DIR}/cert-chain.pem -out-priv ${VM_PKI_DIR}/key.pem \
	 	-host spiffe://cluster.local/ns/vmtest/sa/default --mode signer
	cp ${PKI_DIR}/ca-cert.pem ${VM_PKI_DIR}/root-cert.pem

# Install the deb in a docker image, for testing the install process.
# Will use a minimal base image, install all that is needed.
deb/docker: testcert-gen
	mkdir -p ${TARGET_OUT_LINUX}/deb
	cp tools/packaging/deb/Dockerfile tools/packaging/deb/deb_test.sh ${TARGET_OUT_LINUX}/deb
	# Istio configs, for testing istiod running in the VM.
	cp tests/testdata/config/*.yaml ${TARGET_OUT_LINUX}/deb
	# Test case uses a cert that is not available
	# TODO: use a valid path or copy some certificate
	rm ${TARGET_OUT_LINUX}/deb/se-example.yaml
	# Test certificates - can be used to verify connection with an istiod running on the host or
	# in a separate container.
	cp -a tests/testdata/certs ${TARGET_OUT_LINUX}/deb
	cp ${TARGET_OUT_LINUX}/release/istio-sidecar.deb ${TARGET_OUT_LINUX}/deb/istio-sidecar.deb
	cp ${TARGET_OUT_LINUX}/release/istio.deb ${TARGET_OUT_LINUX}/deb/istio.deb
	docker build -t istio_deb -f ${TARGET_OUT_LINUX}/deb/Dockerfile  ${TARGET_OUT_LINUX}/deb/

# For the test, by default use a local pilot.
# Set it to 172.18.0.1 to run against a pilot running in IDE.
# You may need to enable 15007 in the local machine firewall for this to work.
DEB_PILOT_IP ?= 127.0.0.1
DEB_CMD ?= /bin/bash
ISTIO_NET ?= 172.18
DEB_IP ?= ${ISTIO_NET}.0.3
DEB_PORT_PREFIX ?= 1600

# TODO: docker compose ?


# Run the docker image including the installed debian, with access to all source
# code. Useful for debugging/experiments with iptables.
#
# Before running:
# docker network create --subnet=172.18.0.0/16 istiotest
# The IP of the docker matches the byon-docker service entry in the static configs, if testing without k8s.
#
# On host, run istiod (can be standalone), using Kind or real K8S cluster:
#
# export TOKEN_ISSUER=https://localhost # Dummy, to ignore missing token. Can be real OIDC server.
# export MASTER_ELECTION=false
# istiod discovery -n istio-system
#
deb/run/docker:
	docker run --cap-add=NET_ADMIN --rm \
	  -v ${GO_TOP}:${GO_TOP} \
      -w ${PWD} \
      --mount type=bind,source="$(HOME)/.kube",destination="/home/.kube" \
      --mount type=bind,source="$(GOPATH)",destination="/ws" \
      --net istiotest --ip ${DEB_IP} \
      --add-host echo:10.1.1.1 \
      --add-host byon.test.istio.io:10.1.1.2 \
      --add-host byon-docker.test.istio.io:10.1.1.2 \
      --add-host istiod.istio-system.svc:${DEB_PILOT_IP} \
      ${DEB_ENV} -e ISTIO_SERVICE_CIDR=10.1.1.0/24 \
      -e ISTIO_INBOUND_PORTS=7070,7072,7073,7074,7075 \
      -e PILOT_CERT_DIR=/var/lib/istio/pilot \
      -p 127.0.0.1:${DEB_PORT_PREFIX}1:15007 \
      -p 127.0.0.1:${DEB_PORT_PREFIX}2:7070 \
      -p 127.0.0.1:${DEB_PORT_PREFIX}3:7072 \
      -p 127.0.0.1:${DEB_PORT_PREFIX}4:7073 \
      -p 127.0.0.1:${DEB_PORT_PREFIX}5:7074 \
      -p 127.0.0.1:${DEB_PORT_PREFIX}6:7075 \
      -p 127.0.0.1:${DEB_PORT_PREFIX}7:15012 \
      -p 127.0.0.1:${DEB_PORT_PREFIX}8:15010 \
      -e GOPATH=${GOPATH} \
      -it istio_deb ${DEB_CMD}

deb/test:
	$(MAKE) deb/run/docker DEB_CMD="deb_test.sh test"

deb/run/debug:
	$(MAKE) deb/run/docker DEB_ENV="-e DEB_PILOT_IP=172.18.0.1"

deb/run/mtls:
	$(MAKE) deb/run/docker DEB_PORT_PREFIX=1620 -e DEB_PILOT_IP=172.18.0.1 DEB_IP=172.18.0.5 DEB_ENV="-e ISTIO_PILOT_PORT=15005 -e ISTIO_CP_AUTH=MUTUAL_TLS"

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
