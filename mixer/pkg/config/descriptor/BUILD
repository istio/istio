package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "descriptor.go",
        "validator.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/config/proto:go_default_library",
        "//mixer/pkg/expr:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_gogo_protobuf//jsonpb:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@io_istio_api//:mixer/v1/config/descriptor",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "descriptor_test.go",
        "validator_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//mixer/pkg/config/proto:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_gogo_protobuf//jsonpb:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@io_istio_api//:mixer/v1/config/descriptor",
    ],
)
