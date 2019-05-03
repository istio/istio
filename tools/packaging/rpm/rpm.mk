rpm: rpm/builder-image rpm/istio rpm/proxy

rpm/istio:
	start=$(date +%s)
	docker run --rm \
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
	end=$(date +%s)
	seconds=$(expr ${end} - ${start})
	echo "jianfeih debug, finish rpm/istio in ${seconds} seconds"

rpm/proxy:
	start=$(date +%s)
	docker run --rm \
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
	end=$(date +%s)
	seconds=$(expr ${end} - ${start})
	echo "jianfeih debug, finish rpm/proxy in ${seconds} seconds"

rpm/builder-image:
	start=$(date +%s)
	docker build -t istio-rpm-builder -f ${PWD}/tools/packaging/rpm/Dockerfile.build ${PWD}/tools/packaging/rpm
	end=$(date +%s)
	seconds=$(expr ${end} - ${start})
	echo "jianfeih debug, finish rpm/builder in ${seconds} seconds"

.PHONY: \
	rpm \
	rpm/istio \
	rpm/proxy \
	rpm/builder-image
