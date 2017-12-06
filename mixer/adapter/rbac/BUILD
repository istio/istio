package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["rbac.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/template/authorization:go_default_library",
        "//mixer/pkg/runtime:go_default_library",
        "//mixer/pkg/status:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_golang_glog//:go_default_library",
    ],
)

