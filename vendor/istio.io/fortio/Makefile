# Makefile to build fortio's docker images as well as short cut
# for local test/install

IMAGES=echosrv # plus the combo image / Dockerfile without ext.

DOCKER_PREFIX := docker.io/istio/fortio
LINTERS_IMAGE := docker.io/fortio/fortio.build:v4

TAG:=$(USER)$(shell date +%y%m%d_%H%M%S)

DOCKER_TAG = $(DOCKER_PREFIX)$(IMAGE):$(TAG)

# Local targets:
install:
	go install ./...

# Local test
test:
	go test -timeout 60s -race ./...

# To debug linters, uncomment
#DEBUG_LINTERS="--debug"

local-lint:
	gometalinter $(DEBUG_LINTERS) \
	--deadline=180s --enable-all --aggregate \
	--exclude=.pb.go --disable=gocyclo --line-length=132 $(LINT_PACKAGES)

# Lint everything by default but ok to "make lint LINT_PACKAGES=./fhttp"
LINT_PACKAGES:=./...
# TODO: do something about cyclomatic complexity
# Note CGO_ENABLED=0 is needed to avoid errors as gcc isn't part of the
# build image
lint:
	docker run -v $(shell pwd):/go/src/istio.io/fortio $(LINTERS_IMAGE) bash -c \
		"cd fortio && time go install $(LINT_PACKAGES) \
		&& time make local-lint LINT_PACKAGES=$(LINT_PACKAGES)"

webtest:
	./Webtest.sh

coverage:
	./.circleci/coverage.sh
	curl -s https://codecov.io/bash | bash

# Docker: Pushes the combo image and the smaller image(s)
all: test install lint docker-version docker-push-internal
	@for img in $(IMAGES); do \
		$(MAKE) docker-push-internal IMAGE=.$$img TAG=$(TAG); \
	done

FILES_WITH_IMAGE:= .circleci/config.yml Dockerfile Dockerfile.echosrv \
	Dockerfile.test Makefile release/Dockerfile.in
# Ran make update-build-image TAG=v1 DOCKER_PREFIX=fortio/fortio
update-build-image:
	$(MAKE) docker-push-internal IMAGE=.build TAG=$(TAG)

# Change . to .. when getting to v10 and up...
update-build-image-tag:
	sed -i .bak -e "s/fortio.build:v./fortio.build:$(TAG)/g" $(FILES_WITH_IMAGE)

docker-version:
	@echo "### Docker is `which docker`"
	@docker version

docker-internal:
	@echo "### Now building $(DOCKER_TAG)"
	docker build -f Dockerfile$(IMAGE) -t $(DOCKER_TAG) .

docker-push-internal: docker-internal
	@echo "### Now pushing $(DOCKER_TAG)"
	docker push $(DOCKER_TAG)

release:
	release/release.sh

authorize:
	gcloud docker --authorize-only --project istio-testing

.PHONY: all docker-internal docker-push-internal docker-version authorize test

.PHONY: install lint install-linters coverage weblint update-build-image

.PHONY: local-lint update-build-image-tag release
