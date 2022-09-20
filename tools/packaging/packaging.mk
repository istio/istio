#remove leading characters since package version expects to start with digit
PACKAGE_VERSION ?= $(shell echo $(VERSION) | sed 's/^[a-z]*-//' | sed 's/-//')

# Creates the proxy debian packages. BUILD_WITH_CONTAINER=1 or in CI/CD (BUILD_WITH_CONTAINER=0)
deb: ${TARGET_OUT_LINUX}/release/istio-sidecar.deb

# fpm likes to add extremely high levels of compression. This is fine for release, but for local runs
# where we are just pushing to a local registry (compressed again!), it adds ~1min to builds.
ifneq ($(FAST_VM_BUILDS),)
DEB_COMPRESSION=--deb-compression=none
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
        $(eval SIDECAR_FILES+=${TARGET_OUT_LINUX}/release/$(EXT)=$(ISTIO_PROXY_HOME)/extensions/$(EXT)) \
        $(eval SIDECAR_CENTOS_7_FILES+=${TARGET_OUT_LINUX}/release/$(EXT)=$(ISTIO_PROXY_HOME)/extensions/$(EXT)))

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
		--architecture "${TARGET_ARCH}" \
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
		--architecture "${TARGET_ARCH}" \
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
		--architecture "${TARGET_ARCH}" \
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

.PHONY: \
	deb \
	deb/fpm \
	rpm/fpm \
	rpm-7/fpm \
	sidecar.deb
