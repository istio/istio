# Copyright 2016 Google Inc. All Rights Reserved.
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

load(
    "//:repositories.bzl",
    "boringssl_repositories",
    "protobuf_repositories",
    "googleapis_repositories",
    "googletest_repositories",
    "grpc_repositories",
    "transcoding_repositories",
)

boringssl_repositories()

protobuf_repositories()

googleapis_repositories()

googletest_repositories()

grpc_repositories(envoy_deps=True)

transcoding_repositories()

load(
    "//contrib/endpoints:repositories.bzl",
    "servicecontrol_client_repositories",
)

servicecontrol_client_repositories()

load(
    "//src/envoy/mixer:repositories.bzl",
    "mixer_client_repositories",
)

mixer_client_repositories()

load(
    "@mixerclient_git//:repositories.bzl",
    "mixerapi_repositories",
)

mixerapi_repositories()

load("//src/envoy:repositories.bzl", "lightstep_repositories")

lightstep_repositories()

# Bind BoringSSL for Envoy
bind(
    name = "ssl",
    actual = "@boringssl//:ssl",
)

git_repository(
    name = "envoy",
    remote = "https://github.com/lyft/envoy.git",
    commit = "090050f9c7c31b014224afe80fe77b422fcf0990",
)

load("@envoy//bazel:repositories.bzl", "envoy_dependencies")

envoy_dependencies(skip_targets=["googletest", "protobuf", "protoc", "lightstep", "ssl"])

new_http_archive(
    name = "docker_ubuntu",
    build_file_content = """
load("@bazel_tools//tools/build_defs/docker:docker.bzl", "docker_build")
docker_build(
  name = "xenial",
  tars = ["xenial/ubuntu-xenial-core-cloudimg-amd64-root.tar.gz"],
  visibility = ["//visibility:public"],
)
""",
    sha256 = "de31e6fcb843068965de5945c11a6f86399be5e4208c7299fb7311634fb41943",
    strip_prefix = "docker-brew-ubuntu-core-e406914e5f648003dfe8329b512c30c9ad0d2f9c",
    type = "zip",
    url = "https://codeload.github.com/tianon/docker-brew-ubuntu-core/zip/e406914e5f648003dfe8329b512c30c9ad0d2f9c",
)


DEBUG_BASE_IMAGE_SHA="3f57ae2aceef79e4000fb07ec850bbf4bce811e6f81dc8cfd970e16cdf33e622"

# See github.com/istio/manager/blob/master/docker/debug/build-and-publish-debug-image.sh
# for instructions on how to re-build and publish this base image layer.
http_file(
    name = "ubuntu_xenial_debug",
    url = "https://storage.googleapis.com/istio-build/manager/ubuntu_xenial_debug-" + DEBUG_BASE_IMAGE_SHA + ".tar.gz",
    sha256 = DEBUG_BASE_IMAGE_SHA,
)

# Following go repositories are for building go integration test for mixer filter.
git_repository(
    name = "io_bazel_rules_go",
    commit = "2d9f328a9723baf2d037ba9db28d9d0e30683938", # Apr 6, 2017 (buildifier fix)
    remote = "https://github.com/bazelbuild/rules_go.git",
)

git_repository(
    name = "org_pubref_rules_protobuf",
    commit = "d42e895387c658eda90276aea018056fcdcb30e4", # Mar 07 2017 (gogo* support)
    remote = "https://github.com/pubref/rules_protobuf",
)

load("//src/envoy/mixer/integration_test:repositories.bzl", "go_mixer_repositories")
go_mixer_repositories()
