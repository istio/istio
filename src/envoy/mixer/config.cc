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

#include "src/envoy/mixer/config.h"
#include "google/protobuf/stubs/status.h"
#include "google/protobuf/util/json_util.h"
#include "include/attributes_builder.h"
#include "src/envoy/mixer/utils.h"

using ::google::protobuf::Message;
using ::google::protobuf::util::Status;
using ::istio::mixer::v1::Attributes;
using ::istio::mixer_client::AttributesBuilder;
using ::istio::mixer::v1::config::client::ServiceConfig;
using ::istio::mixer::v1::config::client::TransportConfig;

namespace Envoy {
namespace Http {
namespace Mixer {
namespace {

const std::string kV2Config("v2");

// The name for the mixer server cluster.
const std::string kDefaultMixerClusterName("mixer_server");

void SetDefaultMixerClusters(TransportConfig *config) {
  if (config->check_cluster().empty()) {
    config->set_check_cluster(kDefaultMixerClusterName);
  }
  if (config->report_cluster().empty()) {
    config->set_report_cluster(kDefaultMixerClusterName);
  }
}

bool ReadV2Config(const Json::Object &json, Message *message) {
  if (!json.hasObject(kV2Config)) {
    return false;
  }
  std::string v2_str = json.getObject(kV2Config)->asJsonString();
  Status status = Utils::ParseJsonMessage(v2_str, message);
  auto &logger = Logger::Registry::getLog(Logger::Id::config);
  if (status.ok()) {
    ENVOY_LOG_TO_LOGGER(logger, info, "V2 mixer client config: {}",
                        message->DebugString());
    return true;
  }
  ENVOY_LOG_TO_LOGGER(
      logger, error,
      "Failed to convert mixer V2 client config, error: {}, data: {}",
      status.ToString(), v2_str);
  return false;
}

}  // namespace

void HttpMixerConfig::Load(const Json::Object &json) {
  ReadV2Config(json, &http_config);

  SetDefaultMixerClusters(http_config.mutable_transport());
}

void TcpMixerConfig::Load(const Json::Object &json) {
  ReadV2Config(json, &tcp_config);

  SetDefaultMixerClusters(tcp_config.mutable_transport());
}

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
