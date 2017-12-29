# Builder version
CI_VERSION ?= go1.9-k8s1.7.4
CI_HUB ?= istio

ci.image:
	docker build -t ${CI_HUB}/ci:$(CI_VERSION) -f Dockerfile .

ci.push:
	docker push "${CI_HUB}/ci:$(CI_VERSION)"

ci.run:
	docker run --rm -u $(shell id -u) -it \
        -v ${GOPATH}:${GOPATH} \
        -w ${PWD} \
        -e USER=${USER} \
		--entrypoint /bin/bash \
        ${CI_HUB}/ci:$(CI_VERSION)


.PHONY: image push