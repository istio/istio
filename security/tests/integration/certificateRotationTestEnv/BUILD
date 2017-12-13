load("@io_bazel_rules_go//go:def.bzl", "go_test")

go_test(
    name = "go_default_test",
    srcs = ["integration_test.go"],
    tags = ["manual"],
    deps = [
        "//security/pkg/pki/ca/controller:go_default_library",
        "//security/tests/integration:go_default_library",
        "//tests/integration/framework:go_default_library",
        "@com_github_golang_glog//:go_default_library",
    ],
)
