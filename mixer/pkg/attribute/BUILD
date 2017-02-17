package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "bag.go",
        "definitionFinder.go",
        "dictionaries.go",
        "manager.go",
        "mutableBag.go",
        "rootBag.go",
        "tracker.go",
    ],
    deps = [
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_istio_api//:mixer/v1",
        "@com_github_istio_api//:mixer/v1/config/descriptor",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = [
        "bag_test.go",
        "dictionaries_test.go",
        "manager_test.go",
        "tracker_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "@com_github_gogo_protobuf//types:go_default_library",
    ],
)
