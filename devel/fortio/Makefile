# Experimental Makefile to build fortio's docker images

IMAGES=fortio echosrv grpcping # plus the combo image / Dockerfile without ext.

DOCKER_PREFIX := gcr.io/istio-testing/fortio

TAG:=$(USER)$(shell date +%y%m%d_%H%M%S)

DOCKER_TAG = $(DOCKER_PREFIX)$(IMAGE):$(TAG)

# Pushes the combo image and the 3 smaller images
all: docker-version docker-push-internal
	@for img in $(IMAGES); do \
		$(MAKE) docker-push-internal IMAGE=.$$img TAG=$(TAG); \
	done

docker-version:
	@echo "### Docker is `which docker`"
	@docker version

docker-internal:
	@echo "### Now building $(DOCKER_TAG)"
	docker build -f Dockerfile$(IMAGE) -t $(DOCKER_TAG) .

docker-push-internal: docker-internal
	@echo "### Now pushing $(DOCKER_TAG)"
	docker push $(DOCKER_TAG)

authorize:
	gcloud docker --authorize-only --project istio-testing

.PHONY: all docker-internal docker-push-internal docker-version authorize
