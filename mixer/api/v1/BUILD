load("@org_pubref_rules_protobuf//go:rules.bzl", "go_proto_library")

go_proto_library(
    # use go_default_library here to prevent need to append lib name on imports
    name = "go_default_library",
    importmap = {
        "google/rpc/status.proto": "google.golang.org/genproto/googleapis/rpc/status",
    },
    imports = [
        "external/com_github_google_protobuf/src",
        "external/com_github_googleapis_googleapis"
    ],
    protos = [ 
        "check.proto",
        "report.proto",
        "quota.proto",
        "service.proto",
    ],
    deps = [
        "@com_github_googleapis_googleapis//:go_status_proto",
        "@com_github_google_go_genproto//googleapis/rpc/status:go_default_library",
        "@com_github_golang_protobuf//protoc-gen-go/descriptor:go_default_library",
        "@com_github_golang_protobuf//protoc-gen-go/plugin:go_default_library",
        "@com_github_golang_protobuf//ptypes/any:go_default_library",
        "@com_github_golang_protobuf//ptypes/duration:go_default_library",
        "@com_github_golang_protobuf//ptypes/empty:go_default_library",
        "@com_github_golang_protobuf//ptypes/struct:go_default_library",
        "@com_github_golang_protobuf//ptypes/timestamp:go_default_library",
        "@com_github_golang_protobuf//ptypes/wrappers:go_default_library",
    ],
    with_grpc = True,       
    verbose = 0,
    visibility = ["//visibility:public"],
)
