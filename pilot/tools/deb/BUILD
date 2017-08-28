load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar", "pkg_deb")

pkg_tar(
    name = "agent-bin",
    files = [
        "//cmd/pilot-agent",
    ],
    mode = "0755",
    package_dir = "/usr/local/bin",
)

pkg_tar(
    name = "debian-data-agent",
    extension = "tar.gz",
    deps = [
        ":agent-bin",
    ],
)

pkg_deb(
    name = "istio-agent",
    architecture = "amd64",
    built_using = "bazel",
    data = ":debian-data-agent",
    description_file = "description-agent",
    homepage = "http://istio.io",
    maintainer = "The Istio Authors <istio-dev@googlegroups.com>",
    package = "istio-agent",
    version = "0.2.1",
)

