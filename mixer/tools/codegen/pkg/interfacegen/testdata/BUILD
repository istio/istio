package(default_visibility = ["//tools/codegen/pkg/interfacegen:__pkg__"])

load("@org_pubref_rules_protobuf//protobuf:rules.bzl", "proto_compile")
load("//tools/codegen:generate.bzl", "mixer_proto_library")

mixer_proto_library(
    name = "check",
    testonly = True,
    protos = ["CheckTmpl.proto"],
)

mixer_proto_library(
    name = "report",
    testonly = True,
    protos = ["ReportTmpl.proto"],
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
        "report_proto.descriptor_set",
    ],
)

filegroup(
    name = "generated_files",
    testonly = True,
    srcs = [
        "check_handler.gen.go",
        "check_tmpl.proto",
        "quota_handler.gen.go",
        "quota_tmpl.proto",
        "report_handler.gen.go",
        "report_tmpl.proto",
    ],
)

filegroup(
    name = "golden_files",
    testonly = True,
    srcs = [
        "CheckTmpl.golden.go",
        "CheckTmpl.golden.proto",
        "QuotaTmpl.golden.go",
        "QuotaTmpl.golden.proto",
        "ReportTmpl.golden.go",
        "ReportTmpl.golden.proto",
    ],
)

proto_compile(
    name = "error_template",
    testonly = True,
    args = [
        "--include_imports",
        "--include_source_info",
    ],
    imports = [
        "external/com_github_google_protobuf/src",
    ],
    inputs = [
        "//pkg/adapter/template:protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = ["ErrorTemplate.proto"],
)

filegroup(
    name = "error_test_files",
    testonly = True,
    srcs = [
        "ErrorTemplate.baseline",
        "error_template.descriptor_set",
    ],
)
