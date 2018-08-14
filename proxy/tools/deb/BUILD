# Copyright 2017 Istio Authors. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
################################################################################
#
load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar", "pkg_deb")

# TODO: decide the proper location for binaries and configs and update the file.
# Current layout for binaries matches 0.1 and docker images.

pkg_tar(
    name = "envoy-bin",
    files = [
        "//proxy/src/envoy",
    ],
    mode = "0755",
    package_dir = "/usr/local/bin",
)

pkg_tar(
    name = "istio-conf",
    files = [
        "envoy.json",
        "sidecar.env",
    ],
    mode = "0755",
    package_dir = "/var/lib/istio/envoy",
)

pkg_tar(
    name = "debian-data",
    extension = "tar.gz",
    deps = [
        ":envoy-bin",
        ":istio-conf",
    ],
    tags = ["manual"],
)

pkg_deb(
    name = "istio-proxy",
    architecture = "amd64",
    built_using = "bazel",
    conffiles_file = "conffiles",
    data = ":debian-data",
    description_file = "description",
    homepage = "http://istio.io",
    maintainer = "The Istio Authors <istio-dev@googlegroups.com>",
    package = "istio-proxy",
    postinst = "postinst.sh",
    tags = ["manual"],
    version_file = "//:deb_version",
)
