package(default_visibility=["//visibility:public"])

licenses(["notice"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_library", "go_test")

go_prefix("istio.io/mixer/pkg/aspect/denyChecker")

ISTIO_DEPS = [
    "//:mixer/api/v1",
    "//:istio/config/v1",
    "//:istio/config/v1/aspect/denyChecker"
]

DEPS = [
    "//pkg/aspect:go_default_library",
    "@com_github_golang_protobuf//proto:go_default_library",
    "@com_github_google_go_genproto//googleapis/rpc/code:go_default_library",
    "@com_github_google_go_genproto//googleapis/rpc/status:go_default_library",
    "//pkg/attribute:go_default_library",
    "//pkg/expr:go_default_library"] + ["@com_github_istio_api" + d for d in ISTIO_DEPS]

SRCS = glob(
    include=["*.go"],
    exclude=["*_test.go"],
)

go_library(
    name="go_default_library",
    srcs=SRCS,
    deps=DEPS
)

go_test(
    name="manager_test",
    srcs=SRCS + ["manager_test.go"],
    deps=DEPS,
    size="small",
)
