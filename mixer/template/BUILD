load("@io_bazel_rules_go//go:def.bzl", "go_library")

package(default_visibility = ["//visibility:public"])

go_library(
    name = "go_default_library",
    srcs = [
        "doc.go",
        "template.gen.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/adapter/kubernetesenv/template:go_default_library",
        "//mixer/adapter/servicecontrol/template/servicecontrolreport:go_default_library",
        "//mixer/pkg/adapter:go_default_library",
        "//mixer/pkg/attribute:go_default_library",
        "//mixer/pkg/config/proto:go_default_library",
        "//mixer/pkg/expr:go_default_library",
        "//mixer/pkg/template:go_default_library",
        "//mixer/template/apikey:go_default_library",
        "//mixer/template/authorization:go_default_library",
        "//mixer/template/checknothing:go_default_library",
        "//mixer/template/listentry:go_default_library",
        "//mixer/template/logentry:go_default_library",
        "//mixer/template/metric:go_default_library",
        "//mixer/template/quota:go_default_library",
        "//mixer/template/reportnothing:go_default_library",
        "//mixer/template/tracespan:go_default_library",
        "//pkg/log:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@io_istio_api//mixer/v1/config/descriptor:go_default_library",
        "@io_istio_api//mixer/v1/template:go_default_library",
    ],
)

