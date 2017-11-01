load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["echo.go"],
    visibility = ["//visibility:private"],
    deps = [
        "//pilot/test/grpcecho:go_default_library",
        "@com_github_gorilla_websocket//:go_default_library",
        "@com_github_spf13_pflag//:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_google_grpc//credentials:go_default_library",
        "@org_golang_google_grpc//metadata:go_default_library",
        "@org_golang_x_net//context:go_default_library",
    ],
)

go_binary(
    name = "server",
    library = ":go_default_library",
    visibility = ["//visibility:public"],
)
