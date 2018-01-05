package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "doc.go",
        "template.gen.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/attribute:go_default_library",
        "//mixer/pkg/config/proto:go_default_library",
        "//mixer/pkg/expr:go_default_library",
        "//mixer/pkg/template:go_default_library",
        "//mixer/template/sample/apa:go_default_library",
        "//mixer/template/sample/check:go_default_library",
        "//mixer/template/sample/quota:go_default_library",
        "//mixer/template/sample/report:go_default_library",
        "//pkg/log:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@io_istio_api//mixer/v1/config/descriptor:go_default_library",
        "@io_istio_api//mixer/v1/template:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["template.gen_test.go"],
    library = ":go_default_library",
    deps = [
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/attribute:go_default_library",
        "//mixer/pkg/config/descriptor:go_default_library",
        "//mixer/pkg/config/proto:go_default_library",
        "//mixer/pkg/expr:go_default_library",
        "//mixer/pkg/il/evaluator:go_default_library",
        "//mixer/template/sample/apa:go_default_library",
        "//mixer/template/sample/check:go_default_library",
        "//mixer/template/sample/quota:go_default_library",
        "//mixer/template/sample/report:go_default_library",
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_golang_protobuf//ptypes/empty:go_default_library",
        "@io_istio_api//mixer/v1:go_default_library",
        "@io_istio_api//mixer/v1/config/descriptor:go_default_library",
        "@io_istio_api//mixer/v1/template:go_default_library",
        "@io_istio_gogo_genproto//googleapis/google/rpc:go_default_library",
    ],
)
