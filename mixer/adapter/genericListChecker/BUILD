package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "aspect.go",
        "adapter.go",
        "cfg.pb.go",
    ],
    deps = [
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "//pkg/adapter:go_default_library",
        "//pkg/aspect:go_default_library",
        "//pkg/aspectsupport:go_default_library",
        "//pkg/aspect/listChecker:go_default_library",
    ],
)

go_test(
    name = "generic_list_checker_test",
    size = "small",
    srcs = ["adapter_test.go"],
    library = ":go_default_library",
    deps = ["//pkg/adaptertesting:go_default_library"],
)

load("@org_pubref_rules_protobuf//go:rules.bzl", "go_proto_library")

go_proto_library(
    name = "genericListChecker",
    protos = [
        "cfg.proto",
    ],
    output_to_workspace = False,
    verbose = 0,
    visibility = ["//visibility:public"],
)
