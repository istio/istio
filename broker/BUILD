package(default_visibility = ["//visibility:public"])

licenses(["notice"])

load("@io_bazel_rules_go//go:def.bzl", "gazelle", "go_prefix")

go_prefix("istio.io/broker")

gazelle(
    name = "gazelle",
    args = [
        "-build_file_name",
        "BUILD",
    ],
)
