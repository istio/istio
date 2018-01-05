package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "appoptics.go",
		"log_handler.go",
		"metrics_handler.go",
    ],
    importpath = "istio.io/istio/mixer/adapter/appoptics",
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/adapter/appoptics/appoptics:go_default_library",
        "//mixer/adapter/appoptics/config:go_default_library",
        "//mixer/adapter/appoptics/papertrail:go_default_library",
        "//mixer/adapter/appoptics/promadapter:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/template/logentry:go_default_library",
        "//mixer/template/metric:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "appoptics_test.go",
		"log_handler_test.go",
		"metrics_handler_test.go",
		"adapter_env_test.go",
    ],
    importpath = "istio.io/istio/mixer/adapter/appoptics",
    library = ":go_default_library",
    deps = ["//mixer/pkg/adapter/test:go_default_library"],
)
