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

#include "proxy/src/envoy/utils/config.h"
#include "common/common/logger.h"
#include "google/protobuf/stubs/status.h"
#include "google/protobuf/util/json_util.h"
#include "proxy/src/envoy/utils/utils.h"

using ::google::protobuf::Message;
using ::google::protobuf::util::Status;
using ::istio::mixer::v1::config::client::TransportConfig;

namespace Envoy {
namespace Utils {
namespace {

const std::string kV2Config("v2");
const std::string kV1Config("v1");

// The name for the mixer server cluster.
const std::string kDefaultMixerClusterName("mixer_server");

// ReadConfig() finds config from |json| that matches version |config_version|,
// and parses config into |message|. Returns true if config is read and parsed
// successfully.
bool ReadConfig(const Json::Object &json, const std::string &config_version,
                Message *message) {
  if (!json.hasObject(config_version)) {
    return false;
  }

  std::string config_str = json.getObject(config_version)->asJsonString();
  Status status = ParseJsonMessage(config_str, message);
  auto &logger = Logger::Registry::getLog(Logger::Id::config);
  if (status.ok()) {
    ENVOY_LOG_TO_LOGGER(logger, info, "{} mixer client config: {}",
                        config_version, config_str);
    return true;
  }
  ENVOY_LOG_TO_LOGGER(
      logger, error,
      "Failed to convert mixer {} client config, error: {}, data: {}",
      config_version, status.ToString(), config_str);
  return false;
}

}  // namespace

void SetDefaultMixerClusters(TransportConfig *config) {
  if (config->check_cluster().empty()) {
    config->set_check_cluster(kDefaultMixerClusterName);
  }
  if (config->report_cluster().empty()) {
    config->set_report_cluster(kDefaultMixerClusterName);
  }
}

bool ReadV2Config(const Json::Object &json, Message *message) {
  return ReadConfig(json, kV2Config, message);
}

bool ReadV1Config(const Json::Object &json, Message *message) {
  return ReadConfig(json, kV1Config, message);
}

}  // namespace Utils
}  // namespace Envoy
