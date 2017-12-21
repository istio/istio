load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library")
load("@io_bazel_rules_go//go:def.bzl", "go_library")
load("@com_google_protobuf//:protobuf.bzl", "cc_proto_library")

cc_proto_library(
    name = "cc_proto",
    srcs = glob(
        ["*.proto"],
    ),
    default_runtime = "//external:protobuf",
    protoc = "//external:protoc",
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/v1:cc_proto",
    ],
)

go_library(
    name = "go_default_library",
    srcs = [
        "api_spec.pb.go",
        "auth.pb.go",
        "client_config.pb.go",
        "quota.pb.go",
        "service.pb.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/v1:go_default_library",
        "@com_github_gogo_protobuf//gogoproto:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_gogo_protobuf//sortkeys:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
    ],
)

filegroup(
    name = "go_default_library_protos",
    srcs = [
        "api_spec.proto",
        "auth.proto",
        "client_config.proto",
        "quota.proto",
        "service.proto",
    ],
    visibility = ["//visibility:public"],
)
