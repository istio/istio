package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "template.gen.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/adapter/template:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/expr:go_default_library",
        "//pkg/status:go_default_library",
        "//pkg/template:go_default_library",
        "//template/sample/check:go_default_library",
        "//template/sample/quota:go_default_library",
        "//template/sample/report:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_istio_api//:mixer/v1/config/descriptor",  # keep
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
        "//pkg/adapter:go_default_library",
        "//pkg/adapter/template:go_default_library",
        "//pkg/expr:go_default_library",
        "//template/sample/check:go_default_library",
        "//template/sample/quota:go_default_library",
        "//template/sample/report:go_default_library",
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_golang_protobuf//ptypes/empty:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_istio_api//:mixer/v1/config/descriptor",  # keep
    ],
)
