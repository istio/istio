# Create the docker container used to build the rpm
docker/centos-fpm:
	docker build -t centos-fpm -f tools/rpm/Dockerfile.centos-fpm tools/rpm

# Build the rpm, inside the centos-fpm container.
# Will mount the current dir - not working yet in Circle (needs refactoring)
rpm/build-in-docker:
	rm -f ${ISTIO_OUT}/istio-sidecar.rpm
	(cd ${TOP}; docker run --rm -u $(shell id -u) -it \
        -v ${GO_TOP}:${GO_TOP} \
        -w ${PWD} \
        -e USER=${USER} \
        -e GOPATH=${GOPATH} \
        centos-fpm \
              	fpm -s dir -t rpm -n ${ISTIO_DEB_NAME} -p ${ISTIO_OUT}/istio-sidecar.rpm \
              	    --version $(DEB_VERSION) -C ${GO_TOP} -f \
              		--url http://istio.io  \
              		--license Apache \
              		--vendor istio.io \
              		--maintainer istio@istio.io \
              		--after-install tools/rpm/postinst.sh \
              		--config-files /var/lib/istio/envoy/envoy_bootstrap_tmpl.json \
              		--config-files /var/lib/istio/envoy/sidecar.env \
              		--description "Istio Sidecar" \
              		--depends iproute \
              		--depends iptables \
              		$(SIDECAR_FILES) )

# Create an istio-proxy container based on centos. Unlike deb/ubuntu, this is
# created by installing the rpm.
docker/istio-proxy-centos:
	mkdir -p ${OUT_DIR}/rpm
	cp ${ISTIO_OUT}/istio-sidecar.rpm ${OUT_DIR}/rpm
	cp tools/rpm/Dockerfile.centos ${OUT_DIR}/rpm
	docker build -t istio-proxy-centos -f ${OUT_DIR}/rpm/Dockerfile.centos ${OUT_DIR}/rpm

