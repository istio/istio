load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library")

gogoslick_proto_library(
    name = "go_default_library",
    importmap = {
        "google/protobuf/descriptor.proto": "github.com/gogo/protobuf/protoc-gen-gogo/descriptor",
    },
    imports = [
        "external/com_google_protobuf/src",
        "../../external/com_google_protobuf/src",
    ],
    inputs = [
        "@com_google_protobuf//:well_known_protos",
    ],
    protos = [":protos"],
    verbose = 0,
    visibility = ["//visibility:public"],
    with_grpc = False,
    deps = [
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_gogo_protobuf//protoc-gen-gogo/descriptor:go_default_library",
        "@com_github_gogo_protobuf//sortkeys:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
    ],
)

filegroup(
    name = "protos",
    srcs = ["extensions.proto"],
    visibility = ["//visibility:public"],
)
