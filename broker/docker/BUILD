load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")
load("//docker:broker_docker.bzl", "broker_docker_build")

# Use "manual" target tag to skip rules in the wildcard expansion

pkg_tar(
    name = "broker_tar",
    extension = "tar.gz",
    files = [
        "//cmd/brks",
    ],
    mode = "0755",
    package_dir = "/usr/local/bin/",
    tags = ["manual"],
)

broker_docker_build(
    cmd = [
        "--logtostderr",
        "--v=2",
    ],
    entrypoint = [
        "/usr/local/bin/brks",
        "server",
    ],
    images = [
        {
            "name": "broker",
            "base": "@docker_ubuntu//:xenial",
        },
        {
            "name": "broker_debug",
            "base": "@ubuntu_xenial_debug//file",
        },
    ],
    ports = [
        "9091",
        "9093",
    ],
    repository = "istio",
    tags = ["manual"],
    tars = [
        ":broker_tar",
    ],
)
