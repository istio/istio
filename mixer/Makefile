GO_MINOR_VERSION = $(shell go version | cut -d . -f 2)

.PHONY: docker
.INTERMEDIATE: check.prereqs

default: docker

check.prereqs:
	@if test $(GO_MINOR_VERSION) -lt 9; then echo -n "go version 1.9+ required, found: "; go version; exit 1; fi

clean:
	rm -f docker/mixs
	rm -f docker/ca-certificates.tgz
	$(MAKE) clean -C example/servicegraph


docker: docker/mixs docker/ca-certificates.tgz
	cd docker && docker build -t mixer -f Dockerfile .
	cd docker && docker build -t mixer_debug -f Dockerfile.debug .
	$(MAKE) docker -C example/servicegraph

# fetch debian ca-certs to package in the docker container.
docker/ca-certificates.tgz:
	cp ../docker/ca-certificates.tgz ./docker

docker/mixs: check.prereqs
	../bin/gobuild.sh $@ istio.io/istio/mixer/pkg/version ./cmd/mixs
