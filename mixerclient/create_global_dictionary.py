#!/usr/bin/python
#
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

import sys

TOP = r"""/* Copyright 2017 Istio Authors. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

#include "src/global_dictionary.h"

namespace istio {
namespace mixer_client {
namespace {

/*
 * Automatically generated global dictionary from
 * https://github.com/istio/api/blob/master/mixer/v1/global_dictionary.yaml
 * by run:
 *  ./create_global_dictionary.py \
 *    bazel-mixerclient/external/mixerapi_git/mixer/v1/global_dictionary.yaml \
 *      > src/global_dictionary.cc
 */

const std::vector<std::string> kGlobalWords{
"""

BOTTOM = r"""};

}  // namespace

const std::vector<std::string>& GetGlobalWords() { return kGlobalWords; }

}  // namespace mixer_client
}  // namespace istio"""

all_words = ''
with open(sys.argv[1]) as src_file:
    for line in src_file:
        if line.startswith("-"):
            all_words += "    \"" + line[1:].strip() + "\",\n"

print TOP + all_words + BOTTOM


