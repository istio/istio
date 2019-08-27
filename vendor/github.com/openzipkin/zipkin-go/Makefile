
.DEFAULT_GOAL := test

.PHONY: test
test:
	go test -v -race -cover ./...

.PHONY: bench
bench:
	go test -v -run - -bench . -benchmem ./...

.PHONY: protoc
protoc:
	protoc --go_out=. proto/v2/zipkin.proto
	protoc --go_out=plugins=grpc:. proto/testing/service.proto

.PHONY: lint
lint:
	# Ignore grep's exit code since no match returns 1.
	echo 'linting...' ; golint ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: all
all: vet lint test bench

.PHONY: example
