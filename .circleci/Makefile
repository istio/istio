# Builder version
CI_VERSION ?= go1.11-k8s1.10.4-helm2.7.2-minikube0.25
CI_HUB ?= istio

ci.image:
	docker build -t "${CI_HUB}/ci:$(CI_VERSION)" -f Dockerfile .

ci.push:
	docker push --config=/tmp/circle "${CI_HUB}/ci:$(CI_VERSION)" 

ci.run:
	docker run --rm -u $(shell id -u) -it \
		-v ${GOPATH}:${GOPATH} \
		-w ${PWD} \
		-e USER=${USER} \
		--entrypoint /bin/bash \
		"${CI_HUB}/ci:$(CI_VERSION)"

.PHONY: ci.image ci.push ci.run
