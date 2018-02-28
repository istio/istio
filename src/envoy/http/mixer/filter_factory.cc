/* Copyright 2018 Istio Authors. All Rights Reserved.
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

#include "src/envoy/http/mixer/filter.h"
#include "common/config/utility.h"
#include "envoy/json/json_object.h"
#include "envoy/registry/registry.h"
#include "envoy/server/filter_config.h"
#include "src/envoy/http/mixer/control_factory.h"
#include "src/envoy/utils/config.h"

using ::istio::mixer::v1::config::client::HttpClientConfig;

namespace Envoy {
namespace Server {
namespace Configuration {

class MixerConfigFactory : public NamedHttpFilterConfigFactory {
 public:
  HttpFilterFactoryCb createFilterFactory(const Json::Object& config_json,
                                          const std::string& prefix,
                                          FactoryContext& context) override {
    HttpClientConfig config_pb;
    if (!Utils::ReadV2Config(config_json, &config_pb)) {
      throw EnvoyException("Failed to parse JSON config");
    }

    return createFilterFactory(config_pb, prefix, context);
  }

  HttpFilterFactoryCb createFilterFactoryFromProto(
      const Protobuf::Message& proto_config, const std::string& prefix,
      FactoryContext& context) override {
    return createFilterFactory(
        dynamic_cast<const HttpClientConfig&>(proto_config), prefix, context);
  }

  ProtobufTypes::MessagePtr createEmptyConfigProto() override {
    return ProtobufTypes::MessagePtr{new HttpClientConfig};
  }
  std::string name() override { return "mixer"; }

 private:
  HttpFilterFactoryCb createFilterFactory(const HttpClientConfig& config_pb,
                                          const std::string&,
                                          FactoryContext& context) {
    std::unique_ptr<Http::Mixer::Config> config_obj(
        new Http::Mixer::Config(config_pb));

    HttpFilterFactoryCb auth_filter_cb;
    auto auth_config = config_obj->auth_config();
    if (auth_config) {
      auto& auth_factory =
          Config::Utility::getAndCheckFactory<NamedHttpFilterConfigFactory>(
              std::string("jwt-auth"));
      auto proto_config = auth_factory.createEmptyConfigProto();
      MessageUtil::jsonConvert(*auth_config, *proto_config);
      auth_filter_cb =
          auth_factory.createFilterFactoryFromProto(*proto_config, "", context);
    }

    auto control_factory = std::make_shared<Http::Mixer::ControlFactory>(
        std::move(config_obj), context);
    return [control_factory, auth_filter_cb](
               Http::FilterChainFactoryCallbacks& callbacks) -> void {
      if (auth_filter_cb) {
        auth_filter_cb(callbacks);
      }
      std::shared_ptr<Http::Mixer::Filter> instance =
          std::make_shared<Http::Mixer::Filter>(control_factory->control());
      callbacks.addStreamDecoderFilter(
          Http::StreamDecoderFilterSharedPtr(instance));
      callbacks.addAccessLogHandler(AccessLog::InstanceSharedPtr(instance));
    };
  }
};

static Registry::RegisterFactory<MixerConfigFactory,
                                 NamedHttpFilterConfigFactory>
    register_;

}  // namespace Configuration
}  // namespace Server
}  // namespace Envoy
