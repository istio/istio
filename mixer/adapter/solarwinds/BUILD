package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "solarwinds.go",
		"log_handler.go",
		"metrics_handler.go",
    ],
    importpath = "istio.io/istio/mixer/adapter/solarwinds",
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/adapter/solarwinds/appoptics:go_default_library",
        "//mixer/adapter/solarwinds/config:go_default_library",
        "//mixer/adapter/solarwinds/papertrail:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/template/logentry:go_default_library",
        "//mixer/template/metric:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "solarwinds_test.go",
		"log_handler_test.go",
		"metrics_handler_test.go",
		"adapter_env_test.go",
    ],
    importpath = "istio.io/istio/mixer/adapter/solarwinds",
    library = ":go_default_library",
    deps = ["//mixer/pkg/adapter/test:go_default_library"],
)
