package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "adapter.go",
        "builder.go",
        "listChecker.go",
        "logger.go",
        "metricsReporter.go",
    ],
    deps = [
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_golang_protobuf//ptypes:go_default_library",
        "@com_github_google_go_genproto//googleapis/rpc/code:go_default_library",
        "@com_github_google_go_genproto//googleapis/rpc/status:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_google_grpc//credentials:go_default_library",
    ],
)
