HUB ?=
PROJECT ?= istio

# Using same naming convention as istio/istio
VERSION ?= go1.10-bazel0.15-clang6.0
IMG ?= ci

# Build a local image, can be used for testing with circleci command line.
image:
	docker build -t ${HUB}$(PROJECT)/${IMG}:$(VERSION) -f Dockerfile .

# Push the image to docker
push:
	docker push "${HUB}$(PROJECT)/${IMG}:$(VERSION)"

# Run the image locally, as current user, for debug.
run:
	cd $TOP && docker run -it --rm -u $(id -u) -it \
       -v $PWD:$TOP -w $TOP -e USER=$USER ${HUB}$(PROJECT)/${IMG}:$(VERSION) /bin/bash

.PHONY: image push latest
