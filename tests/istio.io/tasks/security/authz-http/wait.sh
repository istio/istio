#!/bin/bash

# Copyright 2019 Istio Authors
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

for deploy in "productpage-v1" "details-v1" "ratings-v1" "reviews-v1" "reviews-v2" "reviews-v3" "sleep"; do
  if ! kubectl -n default rollout status deployment "$deploy" --timeout 5m
  then
    echo "$deploy deployment rollout status check failed"
    exit 1
  fi
done

exit 0
