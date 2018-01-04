package(default_visibility = ["//mixer/adapter/appoptics:__pkg__","//mixer/adapter/appoptics/appoptics:__pkg__","//mixer/adapter/appoptics/promadapter:__pkg__"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["papertrail_logger.go", "util.go"],
    importpath = "istio.io/istio/mixer/adapter/appoptics/papertrail",
    deps = [
        "//mixer/adapter/appoptics/config:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/pool:go_default_library",
        "//mixer/template/logentry:go_default_library",
    ],
)


go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "papertrail_logger_test.go",
    ],
    importpath = "istio.io/istio/mixer/adapter/appoptics/papertrail",
    library = ":go_default_library",
    deps = ["//mixer/pkg/adapter/test:go_default_library"],
)