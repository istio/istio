# gazelle:ignore
package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "combined.go",
        "@com_github_istio_api//:mixer/v1/config_fixed",  # keep
    ],
    deps = [
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_gogo_protobuf//sortkeys:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_istio_api//:mixer/v1/config/descriptor",  # keep
    ],
)
