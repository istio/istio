load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar", "pkg_deb")

pkg_tar(
    name = "agent-bin",
    files = [
        "//cmd/node_agent",
    ],
    mode = "0755",
    package_dir = "/usr/local/bin",
)

pkg_tar(
    name = "istio-systemd",
    files = ["istio-auth-node-agent.service"],
    mode = "644",
    package_dir = "/lib/systemd/system",
)

pkg_tar(
    name = "debian-data",
    extension = "tar.gz",
    deps = [
        ":agent-bin",
        ":istio-systemd",
    ],
)

pkg_deb(
    name = "istio-auth-node-agent",
    architecture = "amd64",
    built_using = "bazel",
    data = ":debian-data",
    description_file = "description",
    homepage = "http://istio.io",
    maintainer = "The Istio Authors <istio-dev@googlegroups.com>",
    package = "istio-auth-node-agent",
    version = "0.2.1",
)
