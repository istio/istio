package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "noop2.go",
    ],
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/adapter/config:go_default_library",
        "//pkg/status:go_default_library",
        "//pkg/template:go_default_library",
        "//template/sample/report:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = [
        "noop2_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//pkg/adapterManager:go_default_library",
        "//pkg/config:go_default_library",
    ],
)
