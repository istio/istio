load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "framework.go",
        "kubernetes.go",
    ],

    deps = ["@com_github_golang_glog//:go_default_library",
            "//tests/e2e/util:go_default_library",],
    data = glob(["testdata/*"]) + ["//:istio_version"],
    visibility = ["//visibility:public"],
)

go_test(
    name = "go_default_test",
    srcs = ["framework_test.go"],
    library = ":go_default_library",
    data = glob(["testdata/*"]) + ["//:istio_version"],
    visibility = ["//visibility:public"],
)
