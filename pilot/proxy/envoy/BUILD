load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "agent.go",
        "config.go",
        "discovery.go",
        "header.go",
        "policy.go",
        "resources.go",
        "route.go",
        "watcher.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//model:go_default_library",
        "//model/proxy/alphav1/config:go_default_library",
        "@com_github_emicklei_go_restful//:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    srcs = ["route_test.go"],
    library = ":go_default_library",
)
