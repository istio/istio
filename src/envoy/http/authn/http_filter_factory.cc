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
#include "google/protobuf/util/json_util.h"
#include "src/envoy/http/authn/http_filter.h"
#include "src/envoy/utils/utils.h"

using istio::envoy::config::filter::http::authn::v2alpha1::FilterConfig;

namespace Envoy {
namespace Server {
namespace Configuration {

namespace {
// The name for the Istio authentication filter.
const std::string kAuthnFactoryName("istio_authn");
}  // namespace

class AuthnFilterConfig : public NamedHttpFilterConfigFactory,
                          public Logger::Loggable<Logger::Id::filter> {
 public:
  HttpFilterFactoryCb createFilterFactory(const Json::Object& config,
                                          const std::string&,
                                          FactoryContext&) override {
    ENVOY_LOG(debug, "Called AuthnFilterConfig : {}", __func__);

    google::protobuf::util::Status status =
        Utils::ParseJsonMessage(config.asJsonString(), &filter_config_);
    ENVOY_LOG(debug, "Called AuthnFilterConfig : Utils::ParseJsonMessage()");
    if (status.ok()) {
      return createFilter();
    } else {
      ENVOY_LOG(critical, "Utils::ParseJsonMessage() return value is: " +
                              status.ToString());
      throw EnvoyException(
          "In createFilterFactory(), Utils::ParseJsonMessage() return value "
          "is: " +
          status.ToString());
    }
  }

  HttpFilterFactoryCb createFilterFactoryFromProto(
      const Protobuf::Message& proto_config, const std::string&,
      FactoryContext&) override {
    filter_config_ = dynamic_cast<const FilterConfig&>(proto_config);
    return createFilter();
  }

  ProtobufTypes::MessagePtr createEmptyConfigProto() override {
    ENVOY_LOG(debug, "Called AuthnFilterConfig : {}", __func__);
    return ProtobufTypes::MessagePtr{
        new istio::authentication::v1alpha1::Policy};
  }

  std::string name() override { return kAuthnFactoryName; }

 private:
  HttpFilterFactoryCb createFilter() {
    ENVOY_LOG(debug, "Called AuthnFilterConfig : {}", __func__);

    return [&](Http::FilterChainFactoryCallbacks& callbacks) -> void {
      callbacks.addStreamDecoderFilter(
          std::make_shared<Http::Istio::AuthN::AuthenticationFilter>(
              filter_config_));
    };
  }

  FilterConfig filter_config_;
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
