package(default_visibility = ["//visibility:public"])

licenses(["notice"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_library", "go_test")

go_prefix("istio.io/mixer/pkg/aspect")

config_setting(
    name = "local",
    values = {"define": "istio=local"},
)

ISTIO_DEPS = [
    "//:mixer/v1",
    "//:mixer/v1/config",
]

DEPS = [
    "//pkg/adapter:go_default_library",
    "//pkg/aspect/config:go_default_library",
    "//pkg/attribute:go_default_library",
    "//pkg/expr:go_default_library",
    "@com_github_golang_protobuf//jsonpb:go_default_library",
    "@com_github_golang_protobuf//proto:go_default_library",
    "@com_github_golang_protobuf//ptypes/empty:go_default_library",
    "@com_github_golang_protobuf//ptypes/struct:go_default_library",
    "@com_github_google_go_genproto//googleapis/rpc/code:go_default_library",
    "@com_github_istio_api//:mixer/v1/config/descriptor",
] + select({
    ":local": ["@local_istio_api" + d for d in ISTIO_DEPS],
    "//conditions:default": ["@com_github_istio_api" + d for d in ISTIO_DEPS],
})

go_library(
    name = "go_default_library",
    srcs = [
        "denyCheckerManager.go",
        "listCheckerManager.go",
        "loggerManager.go",
        "manager.go",
    ],
    deps = DEPS,
)

go_test(
    name = "denyCheckerManager_test",
    size = "small",
    srcs = [
        "denyCheckerManager_test.go",
        "listCheckerManager_test.go",
        "loggerManager_test.go",
    ],
    library = ":go_default_library",
)
