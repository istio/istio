package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_library", "go_test", "go_binary")

go_library(
    name = "go_default_library",
    srcs = [
        "TemplateExtensions.pb.go",
    ],
    deps = [
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_golang_protobuf//protoc-gen-go/descriptor:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)
