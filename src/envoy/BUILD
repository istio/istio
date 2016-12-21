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
cc_test(
    name = "envoy_test",
    data = [
        "@envoy_git//:envoy-testdata",
    ],
    copts = [
        "-include ./external/envoy_git/test/precompiled/precompiled_test.h",
    ],
    deps = [
        "@envoy_git//:envoy-test-lib",
        "//external:googletest_main",
    ],
    args = [
        #TODO: Make all test pass
        "--gtest_filter=RouterTest.*",
    ],
    linkstatic=1,
)
