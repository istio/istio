package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "runtime.go",
        "validator.go",
    ],
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/config/proto:go_default_library",
        "//pkg/expr:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_mitchellh_mapstructure//:go_default_library",
        "@in_gopkg_yaml_v2//:go_default_library",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = [
        "runtime_test.go",
        "validator_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//pkg/aspectsupport/listChecker/config:go_default_library",
        "@com_github_istio_api//:mixer/v1",
    ],
)
