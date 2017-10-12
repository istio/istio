# Copyright 2017 Istio Authors. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# A Bazel (http://bazel.io) workspace for Istio Mixer client

load(
    "//:repositories.bzl",
    "boringssl_repositories",
    "googletest_repositories",
)

boringssl_repositories()
googletest_repositories()

ISTIO_API = "fadf8937f1e3b88a0a5fb7325d34259f0d65d947" # Oct 11, 2017

git_repository(
    name = "io_istio_api",
    commit = ISTIO_API,
    remote = "https://github.com/istio/api.git",
)

git_repository(
    name = "io_bazel_rules_go",
    commit = "7991b6353e468ba5e8403af382241d9ce031e571",  # Aug 1, 2017 (gazelle fixes)
    remote = "https://github.com/bazelbuild/rules_go.git",
)

load("@io_bazel_rules_go//go:def.bzl", "go_repositories", "go_repository")

go_repositories()

load("@io_istio_api//:api.bzl", "go_istio_api_dependencies")

go_istio_api_dependencies()
