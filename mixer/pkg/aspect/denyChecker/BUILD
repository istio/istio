package(default_visibility = ["//visibility:public"])

licenses(["notice"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_library")

go_prefix("istio.io/mixer/pkg/aspect/denyChecker")

DEPS = [
    "//pkg/aspect:go_default_library",
    "@com_github_golang_protobuf//proto:go_default_library",
    "@com_github_google_go_genproto//googleapis/rpc/status:go_default_library",
]

SRCS = glob(
    include = ["*.go"],
    exclude = ["*_test.go"],
)

go_library(
    name = "go_default_library",
    srcs = SRCS,
    deps = DEPS,
)
