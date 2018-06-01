.DEFAULT_GOAL	:= build

#------------------------------------------------------------------------------
# Variables
#------------------------------------------------------------------------------

SHELL 	:= /bin/bash
BINDIR	:= bin
PKG 		:= github.com/envoyproxy/go-control-plane

# Pure Go sources (not vendored and not generated)
GOFILES		= $(shell find . -type f -name '*.go' -not -path "./vendor/*")
GODIRS		= $(shell go list -f '{{.Dir}}' ./... \
						| grep -vFf <(go list -f '{{.Dir}}' ./vendor/...))

.PHONY: build
build: vendor
	@echo "--> building"
	@go build ./...

.PHONY: clean
clean:
	@echo "--> cleaning compiled objects and binaries"
	@go clean -tags netgo -i ./...
	@rm -rf $(BINDIR)/*

.PHONY: test
test: vendor
	@echo "--> running unit tests"
	@go test ./pkg/...

.PHONY: cover
cover:
	@echo "--> running coverage tests"
	@build/coverage.sh

.PHONY: check
check: format.check vet lint

.PHONY: format
format: tools.goimports
	@echo "--> formatting code with 'goimports' tool"
	@goimports -local $(PKG) -w -l $(GOFILES)

.PHONY: format.check
format.check: tools.goimports
	@echo "--> checking code formatting with 'goimports' tool"
	@goimports -local $(PKG) -l $(GOFILES) | sed -e "s/^/\?\t/" | tee >(test -z)

.PHONY: vet
vet: tools.govet
	@echo "--> checking code correctness with 'go vet' tool"
	@go vet ./...

.PHONY: lint
lint: tools.golint
	@echo "--> checking code style with 'golint' tool"
	@echo $(GODIRS) | xargs -n 1 golint

#-----------------
#-- integration
#-----------------
.PHONY: $(BINDIR)/test $(BINDIR)/test-linux docker integration integration.ads integration.xds integration.rest integration.docker

$(BINDIR)/test: vendor
	@echo "--> building test binary"
	@go build -race -o $@ pkg/test/main/main.go

$(BINDIR)/test-linux: vendor
	@echo "--> building Linux test binary"
	@env GOOS=linux GOARCH=amd64 go build -race -o $@ pkg/test/main/main.go

docker: $(BINDIR)/test-linux
	@echo "--> building test docker image"
	@docker build -t test .

integration: integration.ads integration.xds integration.rest

integration.ads: $(BINDIR)/test
	env XDS=ads build/integration.sh

integration.xds: $(BINDIR)/test
	env XDS=xds build/integration.sh

integration.rest: $(BINDIR)/test
	env XDS=rest build/integration.sh

integration.docker: docker
	docker run -it -e "XDS=ads" test -debug
	docker run -it -e "XDS=xds" test -debug
	docker run -it -e "XDS=rest" test -debug

#-----------------
#-- code generaion
#-----------------

generate: $(BINDIR)/gogofast $(BINDIR)/validate
	@echo "--> generating pb.go files"
	$(SHELL) build/generate_protos.sh

#------------------
#-- dependencies
#------------------
.PHONY: depend.update depend.install

depend.update: tools.glide
	@echo "--> updating dependencies from glide.yaml"
	@glide update

depend.install: tools.glide
	@echo "--> installing dependencies from glide.lock "
	@glide install

vendor:
	@echo "--> installing dependencies from glide.lock "
	@glide install

$(BINDIR):
	@mkdir -p $(BINDIR)

#---------------
#-- tools
#---------------
.PHONY: tools tools.glide tools.goimports tools.golint tools.govet

tools: tools.glide tools.goimports tools.golint tools.govet

tools.goimports:
	@command -v goimports >/dev/null ; if [ $$? -ne 0 ]; then \
		echo "--> installing goimports"; \
		go get golang.org/x/tools/cmd/goimports; \
	fi

tools.govet:
	@go tool vet 2>/dev/null ; if [ $$? -eq 3 ]; then \
		echo "--> installing govet"; \
		go get golang.org/x/tools/cmd/vet; \
	fi

tools.golint:
	@command -v golint >/dev/null ; if [ $$? -ne 0 ]; then \
		echo "--> installing golint"; \
		go get -u golang.org/x/lint/golint; \
	fi

tools.glide:
	@command -v glide >/dev/null ; if [ $$? -ne 0 ]; then \
		echo "--> installing glide"; \
		curl https://glide.sh/get | sh; \
	fi

$(BINDIR)/gogofast: vendor
	@echo "--> building $@"
	@go build -o $@ vendor/github.com/gogo/protobuf/protoc-gen-gogofast/main.go

$(BINDIR)/validate: vendor
	@echo "--> building $@"
	@go build -o $@ vendor/github.com/lyft/protoc-gen-validate/main.go
