load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")

# Use "manual" target tag to skip rules in the wildcard expansion

pkg_tar(
    name = "js_tar",
    extension = "tar.gz",
    files = glob(
        ["**"],
        exclude = ["BUILD"],
    ),
    mode = "0755",
    strip_prefix = "./",
    package_dir = "/examples/servicegraph/js",
    tags = ["manual"],
    visibility = ["//examples/servicegraph/docker:__pkg__"],
)

exports_files(["viz/viz-lite.js"])
