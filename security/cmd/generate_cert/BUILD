load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["main.go"],
    visibility = ["//visibility:private"],
    deps = [
        "//pkg/log:go_default_library",
        "//security/pkg/pki/ca:go_default_library",
        # TODO(nmittler): Remove this
        "@com_github_golang_glog//:go_default_library",
    ],
)

go_binary(
    name = "generate_cert",
    library = ":go_default_library",
    visibility = ["//visibility:public"],
)
