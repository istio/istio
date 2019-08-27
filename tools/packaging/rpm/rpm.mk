rpm: rpm/builder-image rpm/istio rpm/proxy

rpm/istio:
	docker run --rm -it \
				-v ${GO_TOP}:${GO_TOP} \
        -w ${PWD} \
        -e USER=${USER} \
				-e TAG=${TAG} \
				-e ISTIO_GO=${ISTIO_GO} \
				-e ISTIO_OUT=${ISTIO_OUT} \
				-e PACKAGE_VERSION=${PACKAGE_VERSION} \
				-e USER_ID=$(shell id -u) \
				-e GROUP_ID=$(shell id -g) \
				istio-rpm-builder \
				tools/packaging/rpm/build-istio-rpm.sh

rpm/proxy:
	docker run --rm -it \
        -v ${GO_TOP}:${GO_TOP} \
				-w /builder \
        -e USER=${USER} \
				-e ISTIO_ENVOY_VERSION=${ISTIO_ENVOY_VERSION} \
				-e ISTIO_GO=${ISTIO_GO} \
				-e ISTIO_OUT=${ISTIO_OUT} \
				-e PACKAGE_VERSION=${PACKAGE_VERSION} \
				-e USER_ID=$(shell id -u) \
				-e GROUP_ID=$(shell id -g) \
				istio-rpm-builder \
				${PWD}/tools/packaging/rpm/build-proxy-rpm.sh

rpm/builder-image:
	docker build -t istio-rpm-builder -f ${PWD}/tools/packaging/rpm/Dockerfile.build ${PWD}/tools/packaging/rpm

.PHONY: \
	rpm \
	rpm/istio \
	rpm/proxy \
	rpm/builder-image
