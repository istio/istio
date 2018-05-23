# Builder version
CI_VERSION ?= go1.10-k8s1.9-helm2.7.2-minikube0.25-05232018
CI_HUB ?= istio

ci.image:
	# --no-cache ensures go get installs the latest
	docker build --no-cache -t ${CI_HUB}/ci:$(CI_VERSION) -f Dockerfile .

ci.push:
	docker push "${CI_HUB}/ci:$(CI_VERSION)"

ci.run:
	docker run --rm -u $(shell id -u) -it \
        -v ${GOPATH}:${GOPATH} \
        -w ${PWD} \
        -e USER=${USER} \
		--entrypoint /bin/bash \
        ${CI_HUB}/ci:$(CI_VERSION)


.PHONY: ci.image ci.push ci.run
