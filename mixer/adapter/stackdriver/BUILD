load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["stackdriver.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/adapter/stackdriver/config:go_default_library",
        "//mixer/adapter/stackdriver/log:go_default_library",
        "//mixer/adapter/stackdriver/metric:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/template/logentry:go_default_library",
        "//mixer/template/metric:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    srcs = ["stackdriver_test.go"],
    library = ":go_default_library",
    deps = [
        "//mixer/adapter/stackdriver/config:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/adapter/test:go_default_library",
        "//mixer/template/logentry:go_default_library",
        "//mixer/template/metric:go_default_library",
    ],
)
