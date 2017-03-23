load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "config.go",
        "controller.go",
        "conversion.go",
        "secret.go",
        "service.go",
        "validation.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//model/proxy/alphav1/config:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//jsonpb:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
    ],
)

# Auto-generate mock interfaces for unit testing model package.
#
# This should be kept in sync with the `go:generate` directive at the
# top of config_test.go which is necessary for non-bazel testing with
# `go test`.
#
# Add `_test` suffix to the output file so mock interfaces, functions,
# etc. are excluded from test coverage, gometalinter, etc.
genrule(
    name = "genmocks",
    srcs = ["config.go"],
    outs = ["mock_config_gen_test.go"],
    cmd = " mockgen -source $< -destination $@ -package model",
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "config_test.go",
        "mock_config_gen_test.go",
        "service_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//model/proxy/alphav1/config:go_default_library",
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@com_github_golang_mock//gomock:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
    ],
)
