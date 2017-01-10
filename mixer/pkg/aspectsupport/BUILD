package(default_visibility = ["//visibility:public"])

licenses(["notice"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_library")

go_prefix("istio.io/mixer/pkg/aspectsupport")

config_setting(
    name = "local",
    values = {"define": "istio=local"},
)

ISTIO_DEPS = [
    "//:mixer/v1",
    "//:mixer/v1/config",
    "//:mixer/v1/config/aspect",
]

DEPS = [
    "//pkg/aspect:go_default_library",
    "//pkg/attribute:go_default_library",
    "@com_github_google_go_genproto//googleapis/rpc/code:go_default_library",
    "//pkg/expr:go_default_library",
] + select({
    ":local": ["@local_istio_api" + d for d in ISTIO_DEPS],
    "//conditions:default": ["@com_github_istio_api" + d for d in ISTIO_DEPS],
})

go_library(
    name = "go_default_library",
    srcs = glob(
        include = ["*.go"],
        exclude = ["test_*.go"],
    ),
    deps = DEPS,
)
