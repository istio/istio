load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")

# Use "manual" target tag to skip rules in the wildcard expansion

pkg_tar(
    name = "configs_tar",
    srcs = glob([
        "**/*.yml",
    ]),
    extension = "tar.gz",
    mode = "0755",
    package_dir = "/etc/opt/mixer",
    strip_prefix = "./",
    tags = ["manual"],
    visibility = ["//mixer/docker:__pkg__"],
)

filegroup(
    name = "testdata",
    srcs = glob(["**/*.yml"]) + glob(["**/*.yaml"]),
    visibility = ["//mixer/test/testenv:__subpackages__"],
)
