
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
	CGO_ENABLED=0 go build -i -ldflags '-extldflags "-static" -X istio.io/istio/mixer/pkg/version.buildStatus="HAHA"' -o mixs ../cmd/mixs/main.go 
