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

#include "envoy/config/filter/http/authn/v2alpha1/config.pb.h"
#include "envoy/registry/registry.h"
#include "envoy/server/filter_config.h"
#include "google/protobuf/util/json_util.h"
#include "proxy/src/envoy/http/authn/http_filter.h"
#include "proxy/src/envoy/utils/filter_names.h"
#include "proxy/src/envoy/utils/utils.h"

using istio::envoy::config::filter::http::authn::v2alpha1::FilterConfig;

namespace Envoy {
namespace Server {
namespace Configuration {

class AuthnFilterConfig : public NamedHttpFilterConfigFactory,
                          public Logger::Loggable<Logger::Id::filter> {
 public:
  Http::FilterFactoryCb createFilterFactory(const Json::Object& config,
                                            const std::string&,
                                            FactoryContext&) override {
    ENVOY_LOG(debug, "Called AuthnFilterConfig : {}", __func__);
    FilterConfig filter_config;
    google::protobuf::util::Status status =
        Utils::ParseJsonMessage(config.asJsonString(), &filter_config);
    ENVOY_LOG(debug, "Called AuthnFilterConfig : Utils::ParseJsonMessage()");
    if (!status.ok()) {
      ENVOY_LOG(critical, "Utils::ParseJsonMessage() return value is: " +
                              status.ToString());
      throw EnvoyException(
          "In createFilterFactory(), Utils::ParseJsonMessage() return value "
          "is: " +
          status.ToString());
    }
    return createFilterFactory(filter_config);
  }

  Http::FilterFactoryCb createFilterFactoryFromProto(
      const Protobuf::Message& proto_config, const std::string&,
      FactoryContext&) override {
    auto filter_config = dynamic_cast<const FilterConfig&>(proto_config);
    return createFilterFactory(filter_config);
  }

  ProtobufTypes::MessagePtr createEmptyConfigProto() override {
    ENVOY_LOG(debug, "Called AuthnFilterConfig : {}", __func__);
    return ProtobufTypes::MessagePtr{new FilterConfig};
  }

  std::string name() override {
    return Utils::IstioFilterName::kAuthentication;
  }

 private:
  Http::FilterFactoryCb createFilterFactory(const FilterConfig& config_pb) {
    ENVOY_LOG(debug, "Called AuthnFilterConfig : {}", __func__);
    // Make it shared_ptr so that the object is still reachable when callback is
    // invoked.
    // TODO(incfly): add a test to simulate different config can be handled
    // correctly similar to multiplexing on different port.
    auto filter_config = std::make_shared<FilterConfig>(config_pb);
    return
        [filter_config](Http::FilterChainFactoryCallbacks& callbacks) -> void {
          callbacks.addStreamDecoderFilter(
              std::make_shared<Http::Istio::AuthN::AuthenticationFilter>(
                  *filter_config));
        };
  }
};

/**
 * Static registration for the Authn filter. @see RegisterFactory.
 */
static Registry::RegisterFactory<AuthnFilterConfig,
                                 NamedHttpFilterConfigFactory>
    register_;

}  // namespace Configuration
}  // namespace Server
}  // namespace Envoy
