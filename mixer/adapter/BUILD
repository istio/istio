# gazelle:ignore
package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library")
load("//mixer/adapter:inventory.bzl", "inventory_library")

inventory_library(
    name = "go_default_library",
    packages = {
        "denier": "istio.io/istio/mixer/adapter/denier",
        "list": "istio.io/istio/mixer/adapter/list",
        "noop": "istio.io/istio/mixer/adapter/noop",
        "prometheus": "istio.io/istio/mixer/adapter/prometheus",
        "stackdriver": "istio.io/istio/mixer/adapter/stackdriver",
        "statsd": "istio.io/istio/mixer/adapter/statsd",
        "stdio": "istio.io/istio/mixer/adapter/stdio",
        "svcctrl": "istio.io/istio/mixer/adapter/svcctrl",
        "memquota": "istio.io/istio/mixer/adapter/memquota",
    },
    deps = [
        "//mixer/adapter/denier:go_default_library",
        "//mixer/adapter/list:go_default_library",
        "//mixer/adapter/memquota:go_default_library",
        "//mixer/adapter/noop:go_default_library",
        "//mixer/adapter/prometheus:go_default_library",
        "//mixer/adapter/stackdriver:go_default_library",
        "//mixer/adapter/statsd:go_default_library",
        "//mixer/adapter/stdio:go_default_library",
        "//mixer/adapter/svcctrl:go_default_library",
    ],
)
