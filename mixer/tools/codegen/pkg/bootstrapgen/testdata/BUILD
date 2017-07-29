package(default_visibility = ["//tools/codegen/pkg/bootstrapgen:__pkg__"])

load("@org_pubref_rules_protobuf//protobuf:rules.bzl", "proto_compile")
load("//tools/codegen:generate.bzl", "mixer_proto_library")

mixer_proto_library(
    name = "list_template_library",
    testonly = True,
    protos = ["ListTemplate.proto"],
)

mixer_proto_library(
    name = "log_template_library",
    testonly = True,
    protos = ["LogTemplate.proto"],
)

mixer_proto_library(
    name = "metric_template_library",
    testonly = True,
    protos = ["MetricTemplate.proto"],
)

mixer_proto_library(
    name = "quota_template_library",
    testonly = True,
    protos = ["QuotaTemplate.proto"],
)

filegroup(
    name = "descriptors",
    testonly = True,
    srcs = [
        "list_template_library_proto.descriptor_set",
        "log_template_library_proto.descriptor_set",
        "metric_template_library_proto.descriptor_set",
        "quota_template_library_proto.descriptor_set",
    ],
)

filegroup(
    name = "golden_files",
    testonly = True,
    srcs = [
        "AllTemplates.go.golden",
    ],
)
