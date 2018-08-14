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

#include "envoy/registry/registry.h"
#include "envoy/server/filter_config.h"
#include "proxy/src/envoy/tcp/mixer/control_factory.h"
#include "proxy/src/envoy/tcp/mixer/filter.h"

using ::istio::mixer::v1::config::client::TcpClientConfig;

namespace Envoy {
namespace Server {
namespace Configuration {

class FilterFactory : public NamedNetworkFilterConfigFactory {
 public:
  Network::FilterFactoryCb createFilterFactory(
      const Json::Object& config_json, FactoryContext& context) override {
    TcpClientConfig config_pb;
    if (!Utils::ReadV2Config(config_json, &config_pb) &&
        !Utils::ReadV1Config(config_json, &config_pb)) {
      throw EnvoyException("Failed to parse JSON config");
    }

    return createFilterFactory(config_pb, context);
  }

  Network::FilterFactoryCb createFilterFactoryFromProto(
      const Protobuf::Message& config, FactoryContext& context) override {
    return createFilterFactory(dynamic_cast<const TcpClientConfig&>(config),
                               context);
  }

  ProtobufTypes::MessagePtr createEmptyConfigProto() override {
    return ProtobufTypes::MessagePtr{new TcpClientConfig};
  }

  std::string name() override { return "mixer"; }

 private:
  Network::FilterFactoryCb createFilterFactory(const TcpClientConfig& config_pb,
                                               FactoryContext& context) {
    std::unique_ptr<Tcp::Mixer::Config> config_obj(
        new Tcp::Mixer::Config(config_pb));

    auto control_factory = std::make_shared<Tcp::Mixer::ControlFactory>(
        std::move(config_obj), context);
    return [control_factory](Network::FilterManager& filter_manager) -> void {
      std::shared_ptr<Tcp::Mixer::Filter> instance =
          std::make_shared<Tcp::Mixer::Filter>(control_factory->control());
      filter_manager.addReadFilter(Network::ReadFilterSharedPtr(instance));
      filter_manager.addWriteFilter(Network::WriteFilterSharedPtr(instance));
    };
  }
};

static Registry::RegisterFactory<FilterFactory, NamedNetworkFilterConfigFactory>
    register_;

}  // namespace Configuration
}  // namespace Server
}  // namespace Envoy
