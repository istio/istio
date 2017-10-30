package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["util.go"],
    deps = [
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/test/spyAdapter:go_default_library",
        "@com_github_davecgh_go_spew//spew:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    srcs = ["e2e_report_test.go"],
    library = ":go_default_library",
    deps = [
        "//mixer/pkg/template:go_default_library",
        "//mixer/test/spyAdapter:go_default_library",
        "//mixer/test/template:go_default_library",
        "//mixer/test/template/report:go_default_library",
        "//mixer/test/testenv:go_default_library",
        "@io_istio_api//:mixer/v1",
        "@io_istio_api//:mixer/v1/config/descriptor",
    ],
)
