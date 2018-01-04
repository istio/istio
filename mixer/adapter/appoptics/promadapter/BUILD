package(default_visibility = ["//mixer/adapter/appoptics:__pkg__"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "batching.go",
    ],
    importpath = "istio.io/istio/mixer/adapter/appoptics/promadapter",
    visibility = ["//mixer/adapter/appoptics:__pkg__"],
    deps = [
        "//mixer/pkg/adapter:go_default_library",
		"//mixer/adapter/appoptics/config:go_default_library",
        "//mixer/adapter/appoptics/appoptics:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "batching_test.go",
    ],
    importpath = "istio.io/istio/mixer/adapter/appoptics/promadapter",
    library = ":go_default_library",
    deps = [
        "//mixer/pkg/adapter/test:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/adapter/appoptics/appoptics:go_default_library",
		"//mixer/adapter/appoptics/papertrail:go_default_library",
    ],
)
