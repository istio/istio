SHELL="/bin/bash"

GOFILES_NOVENDOR = $(shell go list ./... | grep -v /vendor/)

all: vet fmt docs test

docs:
	./scripts/fixup.sh

fmt:
	go fmt $(GOFILES_NOVENDOR)

vet:
	go vet $(GOFILES_NOVENDOR)

test: vet
	./scripts/test_all.sh

.PHONY: all docs validate test
