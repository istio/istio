# sed parses out the x.y version (of what may be x.y or x.y.z) and outputs "x y".
# awk takes the two separate variables and combines them as a single value x*100+y (e.g., 1.9 is 109).
# This single value allows the major & minor #s to be checked in a single comparison.
GO_VERSION=$(shell go version | sed "s/[a-z| ]*\([0-9]*\)\.\([0-9]*\).*/\1 \2/" | awk '{print $$1*100+$$2}')

# note: docker/mixs to be phony to make sure rebuilding always.
.PHONY: docker docker/mixs
.INTERMEDIATE: check.prereqs

default: docker

check.prereqs:
	@if test $(GO_VERSION) -lt 109; then echo -n "go version 1.9+ required, found: "; go version; exit 1; fi

clean:
	rm -f docker/mixs
	rm -f docker/ca-certificates.tgz
	$(MAKE) clean -C example/servicegraph


docker: docker/mixs docker/ca-certificates.tgz
	cd docker && docker build -t mixer -f Dockerfile .
	cd docker && docker build -t mixer_debug -f Dockerfile.debug .
	$(MAKE) docker -C example/servicegraph

# fetch debian ca-certs to package in the docker container.
docker/ca-certificates.tgz: ../docker/ca-certificates.tgz
	cp ../docker/ca-certificates.tgz ./docker

docker/mixs: check.prereqs
	../bin/gobuild.sh $@ istio.io/istio/mixer/pkg/version ./cmd/mixs
