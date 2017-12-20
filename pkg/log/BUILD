package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "log.go",
        "options.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_spf13_cobra//:go_default_library",
        "@org_golang_google_grpc//grpclog:go_default_library",
        "@org_uber_go_zap//:go_default_library",
        "@org_uber_go_zap//zapcore:go_default_library",
        "@org_uber_go_zap//zapgrpc:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "log_test.go",
        "options_test.go",
    ],
    library = ":go_default_library",
)
