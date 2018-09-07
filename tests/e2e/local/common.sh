#!/bin/bash

# Copyright 2018 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at

#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Common utilities for Istio local installation on any platform.

# read_input_y reads user's input.
# If receives a y|Y, it will return 0, otherwise will return 1.
function read_input_y() {
  read -p "If you are OK with that, press Y|y to continue [default: no]: " -r continue
  ok=${continue:-"no"}
  if [[ $ok = *"y"* ]] || [[ $ok = *"Y"* ]]; then
    return 0
  else
    return 1
  fi
}
