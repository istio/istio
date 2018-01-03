GOBIN ?= go
GO_MINOR_VERSION = $(shell $(GOBIN) version | cut -d . -f 2)

VERSION_MODULE = istio.io/istio/security/cmd/istio_ca/version

.PHONY: docker
.INTERMEDIATE: check.prereqs

default: docker

check.prereqs:
	@if test $(GO_MINOR_VERSION) -lt 9; then echo -n "go version 1.9+ required, found: "; go version; exit 1; fi

clean:
	rm -f docker/istio_ca
	rm -f docker/node_agent
	rm -f docker/ca-certificates.tgz

.PHONY: docker.artifacts
docker.artifacts:
	../bin/gobuild.sh ./docker/istio_ca $(VERSION_MODULE) ./cmd/istio_ca
	../bin/gobuild.sh ./docker/node_agent $(VERSION_MODULE) ./cmd/node_agent
	cp ../docker/ca-certificates.tgz ./docker/

.PHONY: docker
docker: docker.artifacts
	@bin/push-docker ${hub} ${tag} -build-only

.PHONY: docker-push
docker-push: docker.artifacts
	@bin/push-docker ${hub} ${tag}
