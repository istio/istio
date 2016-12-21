package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library", "go_test")

DEPS = [
    "//adapter/denyChecker:go_default_library",
    "//adapter/genericListChecker:go_default_library",
    "//adapter/ipListChecker:go_default_library",
    "//adapter/jsonLogger:go_default_library",
    "//pkg/config/listChecker:go_default_library",
    "//pkg/attribute:go_default_library",
    "//pkg/adapter:go_default_library",
    "@com_github_golang_glog//:go_default_library",
    "@com_github_golang_protobuf//proto:go_default_library",
    "@com_github_golang_protobuf//ptypes:go_default_library",
    "@com_github_google_go_genproto//googleapis/rpc/code:go_default_library",
    "@com_github_google_go_genproto//googleapis/rpc/status:go_default_library",
    "@org_golang_google_grpc//:go_default_library",
    "@org_golang_google_grpc//credentials:go_default_library",
]

go_library(
    name = "go_default_library",
    srcs = [
        "adapterManager.go",
        "configManager.go",
        "dispatchKey.go",
    ],
    deps = DEPS
)
