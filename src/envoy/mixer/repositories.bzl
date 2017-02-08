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

def mixer_client_repositories(bind=True):
    native.git_repository(
        name = "mixerclient_git",
        commit = "7b8544d765f9d7d86d28770c8d27d69cbf9509ac",
        remote = "https://github.com/istio/mixerclient.git",
    )

    if bind:
        native.bind(
            name = "mixer_client_lib",
            actual = "@mixerclient_git//:mixer_client_lib",
        )
