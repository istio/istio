load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")

# Use "manual" target tag to skip rules in the wildcard expansion

pkg_tar(
    name = "js_tar",
    srcs = glob(
        ["**"],
        exclude = ["BUILD"],
    ),
    extension = "tar.gz",
    mode = "0755",
    package_dir = "/example/servicegraph/js",
    strip_prefix = "./",
    tags = ["manual"],
    visibility = ["//mixer/example/servicegraph/docker:__pkg__"],
)

exports_files(["viz/viz-lite.js"])
