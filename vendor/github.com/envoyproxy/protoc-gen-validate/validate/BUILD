# gazelle:exclude validate.pb.go

load("//bazel:go_proto_library.bzl", "go_proto_library")
load("@com_google_protobuf//:protobuf.bzl", "cc_proto_library", "py_proto_library")

proto_library(
    name = "validate_proto",
    srcs = ["validate.proto"],
    deps = [
        "@com_google_protobuf//:descriptor_proto",
        "@com_google_protobuf//:duration_proto",
        "@com_google_protobuf//:timestamp_proto",
    ],
    visibility = ["//visibility:public"],
)

cc_proto_library(
    name = "validate_cc",
    srcs = ["validate.proto"],
    protoc = "@com_google_protobuf//:protoc",
    default_runtime = "@com_google_protobuf//:protobuf",
    deps = ["@com_google_protobuf//:cc_wkt_protos"],
    visibility = ["//visibility:public"],
)

py_proto_library(
    name = "validate_py",
    srcs = ["validate.proto"],
    protoc = "@com_google_protobuf//:protoc",
    default_runtime = "@com_google_protobuf//:protobuf_python",
    deps = ["@com_google_protobuf//:protobuf_python"],
    visibility = ["//visibility:public"],
)

go_proto_library(
    name = "go_default_library",
    srcs = ["validate.proto"],
    protoc = "@com_google_protobuf//:protoc",
    rules_go_repo_only_for_internal_use = "",
    ignore_go_package_option = 1,  # https://github.com/bazelbuild/rules_go/issues/323
    deps = [
        "@com_github_golang_protobuf//ptypes/duration:go_default_library",
        "@com_github_golang_protobuf//ptypes/timestamp:go_default_library",
        "@com_github_golang_protobuf//protoc-gen-go/descriptor:go_default_library",
    ],
    importpath = "github.com/lyft/protoc-gen-validate/validate",
    visibility = ["//visibility:public"],
)

cc_library(
    name = "cc_validate",
    hdrs = ["validate.h"],
    visibility = ["//visibility:public"],
)
