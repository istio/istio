
GO_MINOR_VERSION = $(shell go version | cut -d . -f 2)


.PHONY: docker
.INTERMEDIATE: check.prereqs

default: docker

check.prereqs:
	@if test $(GO_MINOR_VERSION) -lt 9; then echo -n "go version 1.9+ required, found: "; go version; exit 1; fi

docker: mixs ca-certificates.tgz
	docker build -t mixs .

ca-certificates.tgz:
	./fetch_cacerts.sh

mixs: check.prereqs
	../../bin/gobuild.sh mixs istio.io/istio/mixer/pkg/version ../cmd/mixs
