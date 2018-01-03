load("@io_bazel_rules_go//go:def.bzl", "gazelle", "go_prefix")

go_prefix("istio.io/istio")

gazelle(name = "gazelle")

filegroup(
    name = "istio_version",
    srcs = [
        "istio.VERSION",
    ],
    visibility = ["//visibility:public"],
)

genrule(
    name = "deb_version",
    srcs = [],
    outs = ["deb_version.txt"],
    cmd = "echo $${ISTIO_VERSION:-\"0.3.0-dev\"} > \"$@\"",
    visibility = ["//visibility:public"],
)
