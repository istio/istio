load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar", "pkg_deb")
load("@bazel_tools//tools/build_defs/docker:docker.bzl", "docker_build")

genrule(
    name = "kube_agent-linux",
    srcs = ["//:kube_agent_srcs"],
    outs = ["kube_agent"],
    cmd = "env GOOS=linux GOARCH=amd64 go build platform/kube/main/kube_agent.go; cat kube_agent > $@",
)

pkg_tar(
    name = "kube_agent-bin",
    extension = "tar.gz",
    files = [":kube_agent-linux"],
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
