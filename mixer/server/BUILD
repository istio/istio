package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library", "go_test")

DEPS = [
    "//adapters:go_default_library",
    "//adapters/denyChecker:go_default_library",
    "//adapters/factMapper:go_default_library",
    "//adapters/genericListChecker:go_default_library",
    "//adapters/ipListChecker:go_default_library",
    "//api/v1:go_default_library",
    "@com_github_golang_glog//:go_default_library",
    "@com_github_golang_protobuf//proto:go_default_library",
    "@com_github_google_go_genproto//googleapis/rpc/code:go_default_library",
    "@com_github_google_go_genproto//googleapis/rpc/status:go_default_library",
    "@org_golang_google_grpc//:go_default_library",
    "@org_golang_google_grpc//credentials:go_default_library",
]

go_binary(
    name = "server",
    srcs = [ 
        "adapterManager.go",
        "apiHandlers.go",
        "apiServer.go",
        "main.go",
    ],
    deps = DEPS,
)

go_library(
    name = "go_default_library",
    srcs = [
        "adapterManager.go",
        "apiHandlers.go",
        "apiServer.go",
    ],
    deps = DEPS
)

go_test(
    name = "api_server_test",
    srcs = [ "apiServer_test.go" ],
    size = "small",
    deps = [ "//adapters/testutil:go_default_library" ],
    library = ":go_default_library",
)