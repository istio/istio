load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["controller.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//model:go_default_library",
        "//platform:go_default_library",
        "@com_github_golang_glog//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["controller_test.go"],
    library = ":go_default_library",
    deps = [
        "//model:go_default_library",
        "//platform:go_default_library",
        "//test/mock:go_default_library",
    ],
)
