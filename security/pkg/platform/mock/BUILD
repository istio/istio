load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "fakeclient.go",
    ],
    visibility = ["//visibility:public"],
    deps = ["@org_golang_google_grpc//:go_default_library"],
)
