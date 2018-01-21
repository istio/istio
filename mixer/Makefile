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
