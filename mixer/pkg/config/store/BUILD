package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "fsstore.go",
        "store.go",
        "testutil.go",
    ],
    deps = [
        "@com_github_golang_glog//:go_default_library",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = [
        "fsstore_test.go",
        "store_test.go",
    ],
    library = ":go_default_library",
    deps = [
        ":go_default_library",
    ],
)
