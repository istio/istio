load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "ReportTesterTemplate_handler.gen.go",
        "ReportTesterTemplate_instance.pb.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/adapter:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_gogo_protobuf//sortkeys:go_default_library",
        "@io_istio_api//mixer/v1/config/descriptor:go_default_library",
        "@io_istio_api//mixer/v1/template:go_default_library",
    ],
)

filegroup(
    name = "go_default_library_protos",
    srcs = ["ReportTesterTemplate.proto"],
    visibility = ["//visibility:public"],
)

filegroup(
    name = "descriptor_set",
    srcs = ["ReportTesterTemplate_proto.descriptor_set"],
    visibility = ["//visibility:public"],
)
