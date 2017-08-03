load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "nafactory.go",
        "nodeagent.go",
        "onprem.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/pki/ca:go_default_library",
        "//proto:go_default_library",
        "//utils:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_google_grpc//credentials:go_default_library",
        "@org_golang_x_net//context:go_default_library",
    ],
)
