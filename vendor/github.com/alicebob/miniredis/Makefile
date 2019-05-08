.PHONY: all install test testrace vet

all: test vet

install:
	go install

test:
	go test ./...

testrace:
	go test -race ./...

vet:
	go vet ./...
	golint ./...
