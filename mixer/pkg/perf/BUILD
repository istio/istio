load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    testonly = 1,
    srcs = [
        "client.go",
        "clientserver.go",
        "common.go",
        "config.go",
        "controller.go",
        "env.go",
        "load.go",
        "request.go",
        "run.go",
        "server.go",
        "setup.go",
        "standard.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/attribute:go_default_library",
        "//mixer/pkg/server:go_default_library",
        "//mixer/pkg/template:go_default_library",
        "//pkg/log:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@io_istio_api//mixer/v1:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "controller_test.go",
        "request_test.go",
        "run_test.go",
        "setup_test.go",
    ],
    library = ":go_default_library",
)
