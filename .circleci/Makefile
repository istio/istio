# Builder version
CI_HUB ?= istio
CI_VERSION ?= go1.10-k8s1.10.4-helm2.7.2-minikube0.25
LINT_VERSION ?= $(addsuffix -shellcheck5.0,${CI_VERSION})

ci.image:
	docker build -t "${CI_HUB}/ci:$(CI_VERSION)" -f Dockerfile .

ci.push:
	docker push "${CI_HUB}/ci:$(CI_VERSION)"

ci.run:
	docker run --rm -u $(shell id -u) -it \
		-v ${GOPATH}:${GOPATH} \
		-w ${PWD} \
		-e USER=${USER} \
		--entrypoint /bin/bash \
		"${CI_HUB}/ci:$(CI_VERSION)"

lint.image:
	docker build -t "${CI_HUB}/lint:$(LINT_VERSION)" -f lint.dockerfile .

lint.push:
	docker push "${CI_HUB}/lint:$(LINT_VERSION)" 

lint.run:
	docker run --rm -u $(shell id -u) -it \
		-v ${GOPATH}:${GOPATH} \
		-w ${PWD} \
		-e USER=${USER} \
		--entrypoint /bin/bash \
		"${CI_HUB}/lint:$(LINT_VERSION)"

.PHONY: ci.image ci.push ci.run lint.image lint.push lint.run
