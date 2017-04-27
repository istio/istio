load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "agent.go",
        "context.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//model:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//ptypes:go_default_library",
        "@io_istio_api//:go_default_library",
        "@io_k8s_client_go//util/flowcontrol:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["agent_test.go"],
    library = ":go_default_library",
)
