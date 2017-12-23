# Builder version
VERSION ?= go1.9-k8s1.7.4
PROJECT ?= istio
HUB ?=

image:
	docker build -t ${HUB}${PROJECT}/ci:$(VERSION) -f Dockerfile .

push:
	docker push "${HUB}${PROJECT}/ci:$(VERSION)"

run:
	docker run --rm -u $(shell id -u) -it \
        -v ${GOPATH}:${GOPATH} \
        -w ${PWD} \
        -e USER=${USER} \
		--entrypoint /bin/bash \
        ${HUB}${PROJECT}/ci:$(VERSION)


.PHONY: image push