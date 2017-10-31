package(default_visibility = ["//pkg:__subpackages__"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "mock_store.go",
        "resource.go",
        "schema.go",
        "store.go",
    ],
    deps = [
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_mock//gomock:go_default_library",
        "@com_github_golang_protobuf//jsonpb:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
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
    srcs = ["store.go"],
    outs = ["mock_store.go"],
    cmd = "$(location @com_github_golang_mock//mockgen) -source $< -destination $@ -package config",
    tools = ["@com_github_golang_mock//mockgen"],
)

go_test(
    name = "go_default_test",
    srcs = [
        "schema_test.go",
        "store_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@io_istio_api//:go_default_library",
    ],
)
