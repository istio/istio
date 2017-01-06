package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

DEPS = [
    "@com_github_golang_glog//:go_default_library",
    "@com_github_golang_protobuf//proto:go_default_library",
    "@com_github_golang_protobuf//ptypes:go_default_library",
    "@com_github_golang_protobuf//ptypes/timestamp:go_default_library",
    "@com_github_istio_api//:mixer/api/v1",
]

go_library(
    name = "go_default_library",
    srcs = [
        "bag.go",
        "dictionaries.go",
        "manager.go",
        "mutableBag.go",
        "rootBag.go",
        "tracker.go",
    ],
    deps = [
        "@com_github_golang_protobuf//ptypes:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_istio_api//:mixer/api/v1",
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

go_test(
    name = "bag_test",
    size = "small",
    srcs = ["bag_test.go"],
    library = ":go_default_library",
    deps = DEPS,
)

go_test(
    name = "tracker_test",
    size = "small",
    srcs = ["tracker_test.go"],
    library = ":go_default_library",
    deps = DEPS,
)
