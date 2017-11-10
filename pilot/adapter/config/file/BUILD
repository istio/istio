load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "config.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pilot/model:go_default_library",
        "@com_github_golang_glog//:go_default_library",
    ],
)

go_test(
    name = "go_default_xtest",
    size = "small",
    srcs = [
        "config_test.go",
    ],
    data = glob(["testdata/*.golden"]),
    deps = [
        ":go_default_library",
        "//pilot/adapter/config/memory:go_default_library",
        "//pilot/model:go_default_library",
    ],
)
