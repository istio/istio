package(default_visibility = ["//broker/pkg:__subpackages__"])

load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["config.go"],
    deps = [
        "//broker/pkg/model/config:go_default_library",
        "//broker/pkg/testing/mock/proto:go_default_library",
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@io_istio_api//:broker/v1/config",
    ],
)
