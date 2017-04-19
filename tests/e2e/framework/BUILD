load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "framework.go",
        "testInfo.go",
        "kubernetes.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_golang_glog//:go_default_library",
        "@com_github_google_uuid//:go_default_library",
        "@com_google_cloud_go//storage:go_default_library",
        "//tests/e2e/util:go_default_library",
    ],
    data = glob(["testdata/*"]) + ["//:istio_version"],

)

go_test(
    name = "go_default_test",
    srcs = ["framework_test.go"],
    library = ":go_default_library",
    data = glob(["testdata/*"]) + ["//:istio_version"],
    visibility = ["//visibility:public"],
)
