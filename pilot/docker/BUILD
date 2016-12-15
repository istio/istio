load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar", "pkg_deb")
load("@bazel_tools//tools/build_defs/docker:docker.bzl", "docker_build")

pkg_tar(
    name = "kube_agent-bin",
    extension = "tar.gz",
    files = ["//:kube_agent"],
    mode = "0755",
    package_dir = "/",
)

pkg_deb(
    name = "kube_agent-deb",
    architecture = "amd64",
    data = ":kube_agent-bin",
    description = "Istio agent",
    maintainer = "Istio developers",
    package = "kube_agent",
    version = "0.0.1",
)

docker_build(
    name = "kube_agent-docker",
    base = "@docker_debian//:wheezy",
    debs = [":kube_agent-deb"],
    entrypoint = ["/kube_agent"],
)
