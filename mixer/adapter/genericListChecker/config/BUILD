package(default_visibility = ["//visibility:public"])

load("@org_pubref_rules_protobuf//go:rules.bzl", "go_proto_library")

go_proto_library(
    name = "go_default_library",
    protos = [
        "config.proto",
    ],
    verbose = 0,
    visibility = ["//adapter/genericListChecker:__pkg__"],
)
