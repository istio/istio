package(default_visibility = ["//broker/pkg:__subpackages__"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "catalog.go",
        "service.go",
        "serviceBinding.go",
        "serviceInstance.go",
        "servicePlan.go",
    ],
    deps = [
        "@com_github_golang_glog//:go_default_library",
        "@io_istio_api//:broker/v1/config",
    ],
)

go_test(
    name = "go_default_test",
    srcs = [
        "catalog_test.go",
        "service_test.go",
        "serviceplan_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@com_github_golang_mock//gomock:go_default_library",
        "@io_istio_api//:broker/v1/config",
    ],
)
