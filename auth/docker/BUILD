load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")
load("@io_bazel_rules_docker//docker:docker.bzl", "docker_build")

pkg_tar(
    name = "istio_ca_tar",
    extension = "tar.gz",
    srcs = [
        "//auth/cmd/istio_ca:istio_ca",
    ],
    mode = "0755",
    package_dir = "/usr/local/bin",
    tags = [ "manual" ],
)

docker_build(
    base = "@docker_ubuntu//:xenial",
    entrypoint = [
        "/usr/local/bin/istio_ca",
        "--self-signed-ca",
    ],
    name = "istio-ca",
    tars = [ "istio_ca_tar" ],
)
