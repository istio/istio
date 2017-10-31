load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "agent.go",
        "context.go",
        "net.go",
        "resolve.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//model:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//ptypes:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@io_istio_api//:go_default_library",
        "@org_golang_x_time//rate:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["agent_test.go"],
    library = ":go_default_library",
)

go_test(
    name = "go_default_xtest",
    size = "small",
    srcs = ["context_test.go"],
    deps = [
        ":go_default_library",
        "//model:go_default_library",
        "//test/mock:go_default_library",
    ],
)
