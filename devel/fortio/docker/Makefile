# Experimental Makefile to build fortio's echosrv docker image

DOCKER_PREFIX := ldemailly/fortio

TAG:=$(shell date +%y%m%d_%H%M%S)

DOCKER_TAG := $(DOCKER_PREFIX):$(TAG)


docker: echosrv
	@echo Tag is $(TAG)
	docker build -t $(DOCKER_TAG) .

push:
	docker push $(DOCKER_TAG)

echosrv:
	CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-s' istio.io/istio/devel/fortio/cmd/$@

clean:
	rm echosrv

.PHONY: clean docker push

