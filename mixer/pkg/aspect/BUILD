package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "accessLoggerManager.go",
        "denyCheckerManager.go",
        "inventory.go",
        "listCheckerManager.go",
        "loggerManager.go",
        "manager.go",
        "quotaManager.go",
    ],
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/aspect/config:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/config:go_default_library",
        "//pkg/config/proto:go_default_library",
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
        "accessLoggerManager_test.go",
        "denyCheckerManager_test.go",
        "listCheckerManager_test.go",
        "loggerManager_test.go",
        "quotaManager_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//pkg/aspect/test:go_default_library",
    ],
)
