package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "adapter.go",
        "main.go",
        "server.go",
    ],
    visibility = ["//visibility:private"],
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/api:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/server:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
    ],
)

go_binary(
    name = "mixs",
    library = ":go_default_library",
    visibility = ["//visibility:public"],
)
