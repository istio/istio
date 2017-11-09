load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "discovery.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pilot/adapter/config/memory:go_default_library",
        "//pilot/adapter/serviceregistry/aggregate:go_default_library",
        "//pilot/model:go_default_library",
        "//pilot/platform:go_default_library",
        "//pilot/proxy:go_default_library",
        "//pilot/proxy/envoy:go_default_library",
        "//pilot/test/mock:go_default_library",
        "@com_github_golang_protobuf//ptypes:go_default_library",
        "@io_istio_api//:proxy/v1/config",
    ],
)

go_test(
    name = "go_default_xtest",
    size = "small",
    srcs = [
        "discovery_test.go",
    ],
    deps = [
        ":go_default_library",
        "//pilot/adapter/config/file:go_default_library",
        "//pilot/adapter/config/memory:go_default_library",
        "//pilot/model:go_default_library",
    ],
)