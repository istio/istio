load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")
load("//docker:mixer_docker.bzl", "mixer_docker_build")

# Use "manual" target tag to skip rules in the wildcard expansion

pkg_tar(
    name = "mixer_servicegraph_tar",
    extension = "tar.gz",
    files = [
        "//examples/servicegraph/cmd/server:servicegraph",
    ],
    mode = "0755",
    package_dir = "/usr/local/bin/",
    tags = ["manual"],
)

mixer_docker_build(
    images = [
        {"name": "servicegraph",       "base": "@docker_ubuntu//:xenial"},
        {"name": "servicegraph_debug", "base": "@ubuntu_xenial_debug//file"},
    ],
    entrypoint = [
        "/usr/local/bin/servicegraph",
        "--assetDir=/examples/servicegraph",
    ],
    ports = [
        "8088",
    ],
    repository = "istio",
    tags = ["manual"],
    tars = [
        ":mixer_servicegraph_tar",
        "//examples/servicegraph/js:js_tar",
    ],
)
