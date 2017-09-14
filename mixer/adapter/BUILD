package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library")
load(":inventory.bzl", "inventory_library")

go_library(
    name = "go_default_library",
    srcs = [
        "inventory.go",
    ],
    library = ":inventory_lib",  # keep
    visibility = ["//visibility:public"],
    deps = [
        "//adapter/kubernetes:go_default_library",
        "//adapter/noopLegacy:go_default_library",
        "//pkg/adapter:go_default_library",
    ],
)

inventory_library(
    name = "inventory_lib",
    packages = {
        "denier": "istio.io/mixer/adapter/denier",
        "list": "istio.io/mixer/adapter/list",
        "noop": "istio.io/mixer/adapter/noop",
        "prometheus": "istio.io/mixer/adapter/prometheus",
        "stackdriver": "istio.io/mixer/adapter/stackdriver",
        "statsd": "istio.io/mixer/adapter/statsd",
        "stdio": "istio.io/mixer/adapter/stdio",
        "svcctrl": "istio.io/mixer/adapter/svcctrl",
        "memquota": "istio.io/mixer/adapter/memquota2",
    },
    deps = [
        "//adapter/denier:go_default_library",
        "//adapter/list:go_default_library",
        "//adapter/memquota2:go_default_library",
        "//adapter/noop:go_default_library",
        "//adapter/prometheus:go_default_library",
        "//adapter/stackdriver:go_default_library",
        "//adapter/statsd:go_default_library",
        "//adapter/stdio:go_default_library",
        "//adapter/svcctrl:go_default_library",
    ],
)
