load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "appManager.go",
        "framework.go",
        "istioctl.go",
        "kubernetes.go",
        "testInfo.go",
    ],
    data = [
        "//:istio_version",
        "//install/kubernetes",
        "//install/kubernetes/addons",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//tests/e2e/util:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_google_uuid//:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_google_cloud_go//storage:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    srcs = ["framework_test.go"],
    data = ["//:istio_version"],
    library = ":go_default_library",
    visibility = ["//visibility:public"],
)
