package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_test")
load("//tools/codegen:generate.bzl", "mixer_supported_template_library")

mixer_supported_template_library(
    name = "go_default_library",
    packages = {
        "//template/sample/report:go_default_library_proto.descriptor_set": "istio.io/mixer/template/sample/report",
        "//template/sample/check:go_default_library_proto.descriptor_set": "istio.io/mixer/template/sample/check",
        "//template/sample/quota:go_default_library_proto.descriptor_set": "istio.io/mixer/template/sample/quota",
    },
    deps = [
        "//template/sample/check:go_default_library",
        "//template/sample/quota:go_default_library",
        "//template/sample/report:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "template.gen_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//pkg/adapter/template:go_default_library",
        "//pkg/expr:go_default_library",
        "//template/sample/check:go_default_library",
        "//template/sample/quota:go_default_library",
        "//template/sample/report:go_default_library",
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_golang_protobuf//ptypes/empty:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_istio_api//:mixer/v1/config/descriptor",  # keep
    ],
)
