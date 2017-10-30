package(default_visibility = ["//mixer/tools/codegen/pkg/bootstrapgen:__pkg__"])

load("@org_pubref_rules_protobuf//protobuf:rules.bzl", "proto_compile")
load("//mixer/tools/codegen:generate.bzl", "mixer_proto_library")

mixer_proto_library(
    name = "check",
    testonly = True,
    protos = ["CheckTmpl.proto"],
)

mixer_proto_library(
    name = "report2",
    testonly = True,
    protos = ["Report2Tmpl.proto"],
)

mixer_proto_library(
    name = "report1",
    testonly = True,
    protos = ["Report1Tmpl.proto"],
)

mixer_proto_library(
    name = "quota",
    testonly = True,
    protos = ["QuotaTmpl.proto"],
)

filegroup(
    name = "descriptors",
    testonly = True,
    srcs = [
        "check_proto.descriptor_set",
        "quota_proto.descriptor_set",
        "report1_proto.descriptor_set",
        "report2_proto.descriptor_set",
    ],
)

filegroup(
    name = "golden_files",
    testonly = True,
    srcs = [
        "AllTemplates.go.golden",
    ],
)
