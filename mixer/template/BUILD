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
        "//template/checknothing:go_default_library",
        "//template/listentry:go_default_library",
        "//template/logentry:go_default_library",
        "//template/metric:go_default_library",
        "//template/quota:go_default_library",
        "//template/reportnothing:go_default_library",
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
