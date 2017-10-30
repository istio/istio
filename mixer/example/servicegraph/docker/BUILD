load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")
load("//docker:mixer_docker.bzl", "mixer_docker_build")

# Use "manual" target tag to skip rules in the wildcard expansion

pkg_tar(
    name = "mixer_servicegraph_tar",
    extension = "tar.gz",
    files = [
        "//example/servicegraph/cmd/server",
    ],
    mode = "0755",
    package_dir = "/usr/local/bin/",
    symlinks = {"/usr/local/bin/servicegraph": "/usr/local/bin/server"},
    tags = ["manual"],
)

mixer_docker_build(
    entrypoint = [
        "/usr/local/bin/servicegraph",
        "--assetDir=/example/servicegraph",
    ],
    images = [
        {
            "name": "servicegraph",
            "base": "@docker_ubuntu//:xenial",
        },
        {
            "name": "servicegraph_debug",
            "base": "@ubuntu_xenial_debug//file",
        },
    ],
    ports = [
        "8088",
    ],
    repository = "istio",
    tags = ["manual"],
    tars = [
        ":mixer_servicegraph_tar",
        "//example/servicegraph/js:js_tar",
    ],
)
