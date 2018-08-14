# Copyright 2016 Istio Authors. All Rights Reserved.
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

exports_files(["LICENSE"])

config_setting(
    name = "darwin",
    values = {
        "cpu": "darwin",
    },
    visibility = ["//visibility:public"],
)

genrule(
    name = "deb_version",
    srcs = [],
    outs = ["deb_version.txt"],
    cmd = "echo $${ISTIO_VERSION:-\"0.3.0-dev\"} > \"$@\"",
    visibility = ["//visibility:public"],
)
