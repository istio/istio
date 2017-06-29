package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix")
load("@org_pubref_rules_protobuf//go:rules.bzl", "go_proto_library")

go_proto_library(
    name = "go_default_library",
    importmap = {
        "google/protobuf/descriptor.proto": "github.com/golang/protobuf/protoc-gen-go/descriptor",
    },
    imports = [
        "external/com_github_google_protobuf/src",
    ],
    inputs = [
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = ["TemplateExtensions.proto"],
    deps = [
        "@com_github_golang_protobuf//protoc-gen-go/descriptor:go_default_library",
    ],
)

filegroup(
    name = "protos",
    srcs = ["TemplateExtensions.proto"],
)
