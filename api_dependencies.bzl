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

load(":protobuf.bzl", "protobuf_repositories")
load(":cc_gogo_protobuf.bzl", "cc_gogoproto_repositories")
load(":x_tools_imports.bzl", "go_x_tools_imports_repositories")
load(":googleapis.bzl", "googleapis_repositories")
load("@io_bazel_rules_go//go:def.bzl",
     "go_rules_dependencies", "go_register_toolchains", "go_repository")
load("@io_bazel_rules_go//proto:def.bzl", "proto_register_toolchains")


def  mixer_api_dependencies():
    protobuf_repositories(load_repo=True, bind=True)
    cc_gogoproto_repositories()
    go_x_tools_imports_repositories()
    googleapis_repositories()

    go_rules_dependencies()
    go_register_toolchains()

    proto_register_toolchains()

    native.git_repository(
        name = "org_pubref_rules_protobuf",
        commit = "ff3b7e7963daa7cb3b42f8936bc11eda4b960926",  # Oct 03, 2017 (Updating External Import Paths)
        remote = "https://github.com/pubref/rules_protobuf",
    )

    go_repository(
        name = "com_github_golang_protobuf",
        commit = "17ce1425424ab154092bbb43af630bd647f3bb0d",  # Nov 16, 2016 (match pubref dep)
        importpath = "github.com/golang/protobuf",
    )

    go_repository(
        name = "com_github_gogo_protobuf",
        commit = "100ba4e885062801d56799d78530b73b178a78f3",  # Mar 7, 2017 (match pubref dep)
        importpath = "github.com/gogo/protobuf",
        build_file_proto_mode = "legacy",
    )


# proxy has special dependencies
# It has Envoy with its protobuf repository
# It has Mixer for integration tests with go repositiores.
def  mixer_api_for_proxy_dependencies():
    protobuf_repositories(load_repo=False, bind=True)
    cc_gogoproto_repositories()
    googleapis_repositories()

    go_rules_dependencies()
    go_register_toolchains()

    proto_register_toolchains()
