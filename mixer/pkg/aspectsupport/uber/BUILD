package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "env.go",
        "logger.go",
        "manager.go",
    ],
    deps = [
        "//pkg/aspect:go_default_library",
        "//pkg/aspectsupport:go_default_library",
        "//pkg/aspectsupport/denyChecker:go_default_library",
        "//pkg/aspectsupport/listChecker:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/expr:go_default_library",
        "//pkg/registry:go_default_library",
        "@com_github_golang_glog//:go_default_library",
    ],
)

go_test(
    name = "manager_test",
    size = "small",
    srcs = ["manager_test.go"],
    library = ":go_default_library",
    deps = [
        "@com_github_istio_api//:mixer/v1/config",
    ],
)

go_test(
    name = "env_test",
    size = "small",
    srcs = ["env_test.go"],
    library = ":go_default_library",
)
