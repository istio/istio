package(default_visibility = ["//broker/pkg:__subpackages__"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "resource.go",
        "schema.go",
        "store.go",
    ],
    deps = [
        "//pkg/log:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",

        # TODO(nmittler) Remove this
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_mock//gomock:go_default_library",
        "@com_github_golang_protobuf//jsonpb:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@in_gopkg_yaml_v2//:go_default_library",
        "@io_istio_api//broker/v1/config:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    srcs = [
        "mock_store_test.go",
        "schema_test.go",
        "store_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@com_github_golang_mock//gomock:go_default_library",
        "@io_istio_api//broker/v1/config:go_default_library",
    ],
)
