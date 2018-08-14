/* Copyright 2017 Istio Authors. All Rights Reserved.
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

#pragma once

#include "envoy/json/json_object.h"
#include "mixer/v1/config/client/client_config.pb.h"

namespace Envoy {
namespace Utils {

// Set default mixer check_cluster and report_cluster
void SetDefaultMixerClusters(
    ::istio::mixer::v1::config::client::TransportConfig *config);

// Read Mixer filter v2 config.
bool ReadV2Config(const Json::Object &json,
                  ::google::protobuf::Message *message);

// Read Mixer filter v1 config.
bool ReadV1Config(const Json::Object &json,
                  ::google::protobuf::Message *message);
}  // namespace Utils
}  // namespace Envoy
