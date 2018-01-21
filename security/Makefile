## Copyright 2017,2018 Istio Authors
##
## Licensed under the Apache License, Version 2.0 (the "License");
## you may not use this file except in compliance with the License.
## You may obtain a copy of the License at
##
##     http://www.apache.org/licenses/LICENSE-2.0
##
## Unless required by applicable law or agreed to in writing, software
## distributed under the License is distributed on an "AS IS" BASIS,
## WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
## See the License for the specific language governing permissions and
## limitations under the License.

GOBINARY ?= go
GO_MINOR_VERSION = $(shell $(GOBINARY) version | cut -d . -f 2)

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
