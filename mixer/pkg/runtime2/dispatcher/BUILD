load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "dispatcher.go",
        "executor.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        # Only for the dispatcher interface.
        "//mixer/pkg/runtime:go_default_library",

        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/aspect:go_default_library",
        "//mixer/pkg/runtime2/routing:go_default_library",
        "//mixer/pkg/attribute:go_default_library",
        "//mixer/pkg/il/compiled:go_default_library",
        "//pkg/log:go_default_library",
        "//mixer/pkg/pool:go_default_library",
        "//mixer/pkg/status:go_default_library",
        "//mixer/pkg/template:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@io_istio_api//mixer/v1/template:go_default_library",
    ],
)

