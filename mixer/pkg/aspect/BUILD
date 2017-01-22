package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "denyCheckerManager.go",
        "listCheckerManager.go",
        "loggerManager.go",
        "manager.go",
    ],
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/aspect/config:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/expr:go_default_library",
        "@com_github_golang_protobuf//jsonpb:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_golang_protobuf//ptypes/empty:go_default_library",
        "@com_github_golang_protobuf//ptypes/struct:go_default_library",
        "@com_github_google_go_genproto//googleapis/rpc/code:go_default_library",
        "@com_github_istio_api//:mixer/v1",
        "@com_github_istio_api//:mixer/v1/config",
        "@com_github_istio_api//:mixer/v1/config/descriptor",
    ],
)

go_test(
    name = "small_tests",
    size = "small",
    srcs = [
        "denyCheckerManager_test.go",
        "listCheckerManager_test.go",
        "loggerManager_test.go",
    ],
    library = ":go_default_library",
)
