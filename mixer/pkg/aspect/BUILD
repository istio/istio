package(default_visibility=["//visibility:public"])

licenses(["notice"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_library")

go_prefix("istio.io/mixer/pkg/aspect")

ISTIO_DEPS = [
    "//:mixer/api/v1",
    "//:istio/config/v1",
    "//:istio/config/v1/aspect/denyChecker",
    "//:istio/config/v1/aspect/listChecker",
]

DEPS = [
    "@com_github_golang_protobuf//proto:go_default_library",
    "@com_github_google_go_genproto//googleapis/rpc/code:go_default_library",
    "//pkg/adapter:go_default_library",
    "//pkg/attribute:go_default_library",
    "//pkg/expr:go_default_library"] + ["@com_github_istio_api" + d for d in ISTIO_DEPS]

go_library(
    name="go_default_library",
    srcs=glob(
        include=["*.go"],
        exclude=["test_*.go"],
    ),
    deps=DEPS
)
