package(default_visibility = ["//visibility:public"])

licenses(["notice"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_library", "go_test")

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
    "//pkg/aspectsupport:go_default_library",
    "//pkg/aspectsupport/listChecker:go_default_library",
    "//pkg/aspectsupport/denyChecker:go_default_library",
    "//pkg/attribute:go_default_library",
    "//pkg/registry:go_default_library",
    "@com_github_golang_glog//:go_default_library",
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

go_test(
    name = "manager_test",
    size = "small",
    srcs = ["manager_test.go"],
    library = ":go_default_library",
)

go_test(
    name = "env_test",
    size = "small",
    srcs = ["env_test.go"],
    library = ":go_default_library",
)
