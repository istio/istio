PROTOC = build/protoc.$(shell uname)
GLIDE = build/glide.$(shell uname)
PROTOS = api/v1/service.proto api/v1/check.proto api/v1/report.proto api/v1/quota.proto
GO_OUTDIR = api/v1/go
CPP_OUTDIR = api/v1/cpp

all: inst build

build: build_api

clean: clean_api

inst:
	@$(GLIDE) install
	@ if ! which protoc-gen-go > /dev/null; then \
		echo "error: protoc-gen-go not installed" >&2;\
		go get github.com/golang/protobuf/protoc-gen-go;\
	fi

build_api: $(PROTOS)
	@mkdir -p $(GO_OUTDIR) $(CPP_OUTDIR)
	@$(PROTOC) --proto_path=api/v1 --proto_path=vendor --cpp_out=$(CPP_OUTDIR) --go_out=plugins=grpc:$(GO_OUTDIR) $(PROTOS)

clean_api:
	@rm -fr $(GO_OUTDIR) $(CPP_OUTDIR)
