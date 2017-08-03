# Experimental Makefile to build fortio's echosrv docker image

DOCKER_PREFIX := ldemailly/fortio

TAG:=$(shell date +%y%m%d_%H%M%S)

DOCKER_TAG := $(DOCKER_PREFIX):$(TAG)

docker:
	@echo Tag is $(TAG)
	docker build -t $(DOCKER_TAG) .

docker-push: docker
	docker push $(DOCKER_TAG)

# Not used, build directly from Dockerfile

echosrv:
	CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-s' istio.io/istio/devel/fortio/cmd/$@

clean:
	rm echosrv

.PHONY: clean docker push

