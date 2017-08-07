load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "config.go",
        "controller.go",
        "conversion.go",
        "error.go",
        "secret.go",
        "service.go",
        "validation.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//jsonpb:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_golang_protobuf//ptypes:go_default_library",
        "@com_github_golang_protobuf//ptypes/duration:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@in_gopkg_yaml_v2//:go_default_library",
        "@io_istio_api//:go_default_library",
    ],
)

# Auto-generate mock interfaces for unit testing model package.
#
# Add `_test` suffix to the output file so mock interfaces, functions,
# etc. are excluded from test coverage, gometalinter, etc.
genrule(
    name = "genmocks",
    srcs = ["config.go"],
    outs = ["mock_config_gen_test.go"],
    cmd = "$(location @com_github_golang_mock//mockgen) -source $< -destination $@ -package model",
    tools = ["@com_github_golang_mock//mockgen"],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "config_test.go",
        "mock_config_gen_test.go",
        "service_test.go",
        "validation_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@com_github_golang_mock//gomock:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_golang_protobuf//ptypes:go_default_library",
        "@com_github_golang_protobuf//ptypes/duration:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@io_istio_api//:go_default_library",
    ],
)
