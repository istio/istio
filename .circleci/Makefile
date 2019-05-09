# Builder version
CI_VERSION ?= 2019-05-09
CI_HUB ?= istionightly

ci.image:
	docker build -t "${CI_HUB}/ci:$(CI_VERSION)" -f Dockerfile .

ci.push:
	docker push --config=/tmp/circle "${CI_HUB}/ci:$(CI_VERSION)" 

docker.push:
	docker push "${CI_HUB}/ci:$(CI_VERSION)"

ci.run:
	docker run --rm -u $(shell id -u) -it \
		-v ${GOPATH}:${GOPATH} \
		-w ${PWD} \
		-e USER=${USER} \
		--entrypoint /bin/bash \
		"${CI_HUB}/ci:$(CI_VERSION)"

.PHONY: ci.image ci.push ci.run
