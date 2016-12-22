package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

config_setting(
    name = "local",
    values = {"define": "istio=local"},
)

DEPS = [
    "@com_github_golang_glog//:go_default_library",
    "@com_github_golang_protobuf//proto:go_default_library",
    "@com_github_golang_protobuf//ptypes:go_default_library",
    "@com_github_golang_protobuf//ptypes/timestamp:go_default_library",
] + select({
    ":local": ["@local_istio_api//:go_default_library"],
    "//conditions:default": ["@com_github_istio_api//:go_default_library"],
})

go_library(
    name = "go_default_library",
    srcs = [
        "context.go",
        "dictionaries.go",
        "manager.go",
        "tracker.go",
    ],
    deps = [
        "@com_github_golang_protobuf//ptypes:go_default_library",
        "@com_github_istio_api//:go_default_library",
    ],
)

go_test(
    name = "dictionaries_test",
    size = "small",
    srcs = ["dictionaries_test.go"],
    library = ":go_default_library",
)

go_test(
    name = "manager_test",
    size = "small",
    srcs = ["manager_test.go"],
    library = ":go_default_library",
    deps = DEPS,
)
