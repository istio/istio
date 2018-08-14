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

#include "envoy/registry/registry.h"
#include "google/protobuf/util/json_util.h"
#include "src/envoy/http/jwt_auth/auth_store.h"
#include "src/envoy/http/jwt_auth/http_filter.h"
#include "src/envoy/utils/filter_names.h"

using ::istio::envoy::config::filter::http::jwt_auth::v2alpha1::
    JwtAuthentication;

namespace Envoy {
namespace Server {
namespace Configuration {

class JwtVerificationFilterConfig : public NamedHttpFilterConfigFactory {
 public:
  Http::FilterFactoryCb createFilterFactory(const Json::Object& config,
                                            const std::string&,
                                            FactoryContext& context) override {
    JwtAuthentication proto_config;
    MessageUtil::loadFromJson(config.asJsonString(), proto_config);
    return createFilter(proto_config, context);
  }

  Http::FilterFactoryCb createFilterFactoryFromProto(
      const Protobuf::Message& proto_config, const std::string&,
      FactoryContext& context) override {
    return createFilter(dynamic_cast<const JwtAuthentication&>(proto_config),
                        context);
  }

  ProtobufTypes::MessagePtr createEmptyConfigProto() override {
    return ProtobufTypes::MessagePtr{new JwtAuthentication};
  }

  std::string name() override { return Utils::IstioFilterName::kJwt; }

 private:
  Http::FilterFactoryCb createFilter(const JwtAuthentication& proto_config,
                                     FactoryContext& context) {
    auto store_factory = std::make_shared<Http::JwtAuth::JwtAuthStoreFactory>(
        proto_config, context);
    Upstream::ClusterManager& cm = context.clusterManager();
    return [&cm, store_factory](
               Http::FilterChainFactoryCallbacks& callbacks) -> void {
      callbacks.addStreamDecoderFilter(
          std::make_shared<Http::JwtVerificationFilter>(
              cm, store_factory->store()));
    };
  }
};

/**
 * Static registration for this JWT verification filter. @see RegisterFactory.
 */
static Registry::RegisterFactory<JwtVerificationFilterConfig,
                                 NamedHttpFilterConfigFactory>
    register_;

}  // namespace Configuration
}  // namespace Server
}  // namespace Envoy
