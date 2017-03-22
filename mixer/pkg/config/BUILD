package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "manager.go",
        "runtime.go",
        "validator.go",
    ],
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/config/descriptor:go_default_library",
        "//pkg/config/proto:go_default_library",
        "//pkg/expr:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_gogo_protobuf//jsonpb:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = [
        "manager_test.go",
        "runtime_test.go",
        "validator_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//pkg/aspect/config:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_istio_api//:mixer/v1",
        "@com_github_istio_api//:mixer/v1/config/descriptor",
    ],
)
