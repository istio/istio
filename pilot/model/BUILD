load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "config.go",
        "controller.go",
        "conversion.go",
        "egress_rules.go",
        "service.go",
        "validation.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pilot/model/test:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_golang_protobuf//jsonpb:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_golang_protobuf//ptypes:go_default_library",
        "@com_github_golang_protobuf//ptypes/duration:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@in_gopkg_yaml_v2//:go_default_library",
        "@io_istio_api//mesh/v1alpha1:go_default_library",
        "@io_istio_api//mixer/v1:go_default_library",
        "@io_istio_api//mixer/v1/config/client:go_default_library",
        "@io_istio_api//routing/v1alpha1:go_default_library",
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
        "//pilot/model/test:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_golang_protobuf//ptypes:go_default_library",
        "@com_github_golang_protobuf//ptypes/duration:go_default_library",
        "@com_github_golang_protobuf//ptypes/wrappers:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@io_istio_api//mesh/v1alpha1:go_default_library",
        "@io_istio_api//mixer/v1:go_default_library",
        "@io_istio_api//mixer/v1/config/client:go_default_library",
        "@io_istio_api//routing/v1alpha1:go_default_library",
    ],
)

go_test(
    name = "go_default_xtest",
    size = "small",
    srcs = [
        "config_test.go",
        "conversion_test.go",
        "egress_rules_test.go",
    ],
    deps = [
        ":go_default_library",
        "//pilot/adapter/config/memory:go_default_library",
        "//pilot/test/mock:go_default_library",
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@io_istio_api//routing/v1alpha1:go_default_library",
    ],
)
