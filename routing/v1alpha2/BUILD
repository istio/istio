load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "destination_rule.pb.go",
        "gateway.pb.go",
        "route_rule.pb.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_golang_protobuf//ptypes/duration:go_default_library",
        "@com_github_golang_protobuf//ptypes/wrappers:go_default_library",
    ],
)

filegroup(
    name = "go_default_library_protos",
    srcs = [
        "destination_rule.proto",
        "gateway.proto",
        "route_rule.proto",
    ],
    visibility = ["//visibility:public"],
)
