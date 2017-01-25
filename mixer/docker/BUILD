load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")
load("@bazel_tools//tools/build_defs/docker:docker.bzl", "docker_build")

# Use "manual" target tag to skip rules in the wildcard expansion

pkg_tar(
    name = "mixer_tar",
    extension = "tar.gz",
    files = [
        "//cmd/server:mixs",
    ],
    mode = "0755",
    package_dir = "/usr/local/bin/",
    tags = ["manual"],
)

docker_build(
    name = "mixer",
    base = "@docker_ubuntu//:xenial",
    entrypoint = [
        "/usr/local/bin/mixs",
        "server",
    ],
    ports = ["9091"],
    repository = "istio",
    tags = ["manual"],
    tars = [
        ":mixer_tar",
    ],
)
