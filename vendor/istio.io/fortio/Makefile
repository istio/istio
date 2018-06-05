# Makefile to build fortio's docker images as well as short cut
# for local test/install
#
# See also release/README.md
#

IMAGES=echosrv fcurl # plus the combo image / Dockerfile without ext.

DOCKER_PREFIX := docker.io/istio/fortio
BUILD_IMAGE_TAG := v7
BUILD_IMAGE := istio/fortio.build:$(BUILD_IMAGE_TAG)

TAG:=$(USER)$(shell date +%y%m%d_%H%M%S)

DOCKER_TAG = $(DOCKER_PREFIX)$(IMAGE):$(TAG)

CERT_TEMP_DIR := ./cert-tmp/

# go test ./... and others run in vendor/ and cause problems (!)
# so to avoid `can't load package: package istio.io/fortio/...: no Go files in ...`
# note that only go1.8 needs the grep -v vendor but we are compatible with 1.8
PACKAGES:=$(shell go list ./... | grep -v vendor)

# Marker for whether vendor submodule is here or not already
GRPC_DIR:=./vendor/google.golang.org/grpc

# Run dependencies
dependencies: submodule certs

# Local targets:
install: dependencies
	go install $(PACKAGES)

# Only generate certs if needed
certs: $(CERT_TEMP_DIR)/server.cert

# Generate certs for unit and release tests.
$(CERT_TEMP_DIR)/server.cert: cert-gen
	./cert-gen

# Remove certificates
certs-clean:
	rm -rf $(CERT_TEMP_DIR)

# Local test
test: dependencies
	go test -timeout 60s -race $(PACKAGES)

# To debug strange linter errors, uncomment
# DEBUG_LINTERS="--debug"

local-lint: dependencies vendor.check
	gometalinter $(DEBUG_LINTERS) \
	--deadline=180s --enable-all --aggregate \
	--exclude=.pb.go --disable=gocyclo --disable=gas --line-length=132 \
	$(LINT_PACKAGES)

# Lint everything by default but ok to "make lint LINT_PACKAGES=./fhttp"
LINT_PACKAGES:=$(PACKAGES)
# TODO: do something about cyclomatic complexity; maybe reenable gas
# Note CGO_ENABLED=0 is needed to avoid errors as gcc isn't part of the
# build image
lint: dependencies
	docker run -v $(shell pwd):/go/src/istio.io/fortio $(BUILD_IMAGE) bash -c \
		"cd fortio && time go install $(LINT_PACKAGES) \
		&& time make local-lint LINT_PACKAGES=\"$(LINT_PACKAGES)\""

# This really also tests the release process and build on windows,mac,linux
# and the docker images, not just "web" (ui) stuff that it also exercises.
release-test:
	./Webtest.sh

# old name for release-test
webtest: release-test

coverage: dependencies
	./.circleci/coverage.sh
	curl -s https://codecov.io/bash | bash

# Submodule handling when not already there
submodule: $(GRPC_DIR)

$(GRPC_DIR):
	$(MAKE) submodule-sync

# If you want to force update/sync, invoke 'make submodule-sync' directly
submodule-sync:
	git submodule sync
	git submodule update --init

# Short cut for pulling/updating to latest of the current branch
pull:
	git pull
	$(MAKE) submodule-sync

# https://github.com/istio/istio/wiki/Vendor-FAQ#how-do-i-add--change-a-dependency
# PS: for fortio no dependencies should be added, only grpc updated.
depend.status:
	@echo "No error means your Gopkg.* are in sync and ok with vendor/"
	dep status
	cp Gopkg.* vendor/

depend.update.full: depend.cleanlock depend.update

depend.cleanlock:
	-rm Gopkg.lock

depend.update:
	@echo "Running dep ensure with DEPARGS=$(DEPARGS)"
	time dep ensure $(DEPARGS)
	cp Gopkg.* vendor/
	@echo "now check the diff in vendor/ and make a PR"

vendor.check:
	@echo "Checking that Gopkg.* are in sync with vendor/ submodule:"
	@echo "if this fails, 'make pull' and/or seek on-call help"
	diff Gopkg.toml vendor/
	diff Gopkg.lock vendor/

.PHONY: depend.status depend.cleanlock depend.update depend.update.full vendor.check


# Docker: Pushes the combo image and the smaller image(s)
all: test install lint docker-version docker-push-internal
	@for img in $(IMAGES); do \
		$(MAKE) docker-push-internal IMAGE=.$$img TAG=$(TAG); \
	done

# Makefile should be edited first
FILES_WITH_IMAGE:= .circleci/config.yml Dockerfile Dockerfile.echosrv \
	Dockerfile.test Dockerfile.fcurl release/Dockerfile.in /etc/ssl/certs/ca-certificates.crt
# Ran make update-build-image BUILD_IMAGE_TAG=v1 DOCKER_PREFIX=fortio/fortio
update-build-image:
	$(MAKE) docker-push-internal IMAGE=.build TAG=$(BUILD_IMAGE_TAG)

# Change . to .. when getting to v10 and up...
update-build-image-tag:
	sed -i .bak -e 's!istio/fortio.build:v.!$(BUILD_IMAGE)!g' $(FILES_WITH_IMAGE)

docker-version:
	@echo "### Docker is `which docker`"
	@docker version

docker-internal: dependencies
	@echo "### Now building $(DOCKER_TAG)"
	docker build -f Dockerfile$(IMAGE) -t $(DOCKER_TAG) .

docker-push-internal: docker-internal
	@echo "### Now pushing $(DOCKER_TAG)"
	docker push $(DOCKER_TAG)

release: dependencies
	release/release.sh

authorize:
	gcloud docker --authorize-only --project istio-testing

.PHONY: all docker-internal docker-push-internal docker-version authorize test dependencies

.PHONY: install lint install-linters coverage webtest release-test update-build-image

.PHONY: local-lint update-build-image-tag release submodule submodule-sync pull certs certs-clean
