load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "config.go",
        "controller.go",
        "conversion.go",
        "error.go",
        "service.go",
        "validation.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//model/test:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_golang_protobuf//jsonpb:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_golang_protobuf//ptypes:go_default_library",
        "@com_github_golang_protobuf//ptypes/duration:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@in_gopkg_yaml_v2//:go_default_library",
        "@io_istio_api//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "service_test.go",
        "validation_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//model/test:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_golang_protobuf//ptypes:go_default_library",
        "@com_github_golang_protobuf//ptypes/duration:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@io_istio_api//:go_default_library",
    ],
)

go_test(
    name = "go_default_xtest",
    size = "small",
    srcs = ["config_test.go"],
    deps = [
        ":go_default_library",
        "//adapter/config/memory:go_default_library",
        "//test/mock:go_default_library",
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@io_istio_api//:go_default_library",
    ],
)
