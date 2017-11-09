load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")
load("@io_bazel_rules_docker//docker:docker.bzl", "docker_build")

pkg_tar(
    name = "istio_ca_tar",
    srcs = [
        "//security/cmd/istio_ca",
    ],
    extension = "tar.gz",
    mode = "0755",
    package_dir = "/usr/local/bin",
    tags = ["manual"],
)

docker_build(
    name = "istio-ca",
    base = "@docker_ubuntu//:xenial",
    entrypoint = [
        "/usr/local/bin/istio_ca",
        "--self-signed-ca",
    ],
    tars = ["istio_ca_tar"],
)
