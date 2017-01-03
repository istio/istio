package(default_visibility=["//visibility:public"])

licenses(["notice"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_library")

go_prefix("istio.io/mixer/pkg/aspectsupport")

config_setting(
    name="local",
    values={"define": "istio=local"}
)
ISTIO_DEPS = [
    "//:mixer/api/v1",
    "//:istio/config/v1",
    "//:istio/config/v1/aspect/listChecker"
]

DEPS = ["//pkg/aspect:go_default_library",
        "//pkg/aspect/listChecker:go_default_library",
        "//pkg/attribute:go_default_library",
        "//pkg/expr:go_default_library"] + select({
            ":local": ["@local_istio_api" + d for d in ISTIO_DEPS],
            "//conditions:default": ["@com_github_istio_api" + d for d in ISTIO_DEPS]
        })

go_library(
    name="go_default_library",
    srcs=glob(
        include=["*.go"],
        exclude=["test_*.go"],
    ),
    deps=DEPS
)
