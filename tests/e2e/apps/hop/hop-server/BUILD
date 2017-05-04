load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["main.go"],
    visibility = ["//visibility:private"],
    deps = [
        "//tests/e2e/apps/hop:go_default_library",
        "//tests/e2e/apps/hop/config:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
    ],
)

go_binary(
    name = "hop-server",
    library = ":go_default_library",
    visibility = ["//visibility:public"],
)
