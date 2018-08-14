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

#include "common/config/utility.h"
#include "envoy/json/json_object.h"
#include "envoy/registry/registry.h"
#include "envoy/server/filter_config.h"
#include "proxy/src/envoy/http/mixer/control_factory.h"
#include "proxy/src/envoy/http/mixer/filter.h"
#include "proxy/src/envoy/utils/config.h"

using ::istio::mixer::v1::config::client::HttpClientConfig;
using ::istio::mixer::v1::config::client::ServiceConfig;

namespace Envoy {
namespace Server {
namespace Configuration {

class MixerConfigFactory : public NamedHttpFilterConfigFactory {
 public:
  Http::FilterFactoryCb createFilterFactory(const Json::Object& config_json,
                                            const std::string& prefix,
                                            FactoryContext& context) override {
    HttpClientConfig config_pb;
    if (!Utils::ReadV2Config(config_json, &config_pb) &&
        !Utils::ReadV1Config(config_json, &config_pb)) {
      throw EnvoyException("Failed to parse JSON config");
    }

    return createFilterFactory(config_pb, prefix, context);
  }

  Http::FilterFactoryCb createFilterFactoryFromProto(
      const Protobuf::Message& proto_config, const std::string& prefix,
      FactoryContext& context) override {
    return createFilterFactory(
        dynamic_cast<const HttpClientConfig&>(proto_config), prefix, context);
  }

  ProtobufTypes::MessagePtr createEmptyConfigProto() override {
    return ProtobufTypes::MessagePtr{new HttpClientConfig};
  }

  ProtobufTypes::MessagePtr createEmptyRouteConfigProto() override {
    return ProtobufTypes::MessagePtr{new ServiceConfig};
  }

  Router::RouteSpecificFilterConfigConstSharedPtr
  createRouteSpecificFilterConfig(
      const Protobuf::Message& config,
      Envoy::Server::Configuration::FactoryContext&) override {
    auto obj = std::make_shared<Http::Mixer::PerRouteServiceConfig>();
    // TODO: use downcastAndValidate once client_config.proto adds validate
    // rules.
    obj->config = dynamic_cast<const ServiceConfig&>(config);
    obj->hash = std::to_string(MessageUtil::hash(obj->config));
    return obj;
  }

  std::string name() override { return "mixer"; }

 private:
  Http::FilterFactoryCb createFilterFactory(const HttpClientConfig& config_pb,
                                            const std::string&,
                                            FactoryContext& context) {
    std::unique_ptr<Http::Mixer::Config> config_obj(
        new Http::Mixer::Config(config_pb));
    auto control_factory = std::make_shared<Http::Mixer::ControlFactory>(
        std::move(config_obj), context);
    return [control_factory](
               Http::FilterChainFactoryCallbacks& callbacks) -> void {
      std::shared_ptr<Http::Mixer::Filter> instance =
          std::make_shared<Http::Mixer::Filter>(control_factory->control());
      callbacks.addStreamFilter(Http::StreamFilterSharedPtr(instance));
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
