load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "dest_policy.pb.go",
        "egress_rule.pb.go",
        "http_fault.pb.go",
        "ingress_rule.pb.go",
        "l4_fault.pb.go",
        "route_rule.pb.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_golang_protobuf//ptypes/any:go_default_library",
        "@com_github_golang_protobuf//ptypes/duration:go_default_library",
        "@com_github_golang_protobuf//ptypes/wrappers:go_default_library",
    ],
)

filegroup(
    name = "go_default_library_protos",
    srcs = [
        "dest_policy.proto",
        "egress_rule.proto",
        "http_fault.proto",
        "ingress_rule.proto",
        "l4_fault.proto",
        "route_rule.proto",
    ],
    visibility = ["//visibility:public"],
)
