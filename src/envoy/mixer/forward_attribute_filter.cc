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

#include "precompiled/precompiled.h"

#include "common/common/base64.h"
#include "common/common/logger.h"
#include "common/http/headers.h"
#include "common/http/utility.h"
#include "envoy/server/instance.h"
#include "server/config/network/http_connection_manager.h"
#include "src/envoy/mixer/utils.h"

namespace Http {
namespace ForwardAttribute {
namespace {

// The Json object name to specify attributes which will be forwarded
// to the upstream istio proxy.
const std::string kJsonNameAttributes("attributes");

}  // namespace

class Config : public Logger::Loggable<Logger::Id::http> {
 private:
  std::string attributes_;

 public:
  Config(const Json::Object& config) {
    Utils::StringMap attributes =
        Utils::ExtractStringMap(config, kJsonNameAttributes);
    if (!attributes.empty()) {
      std::string serialized_str = Utils::SerializeStringMap(attributes);
      attributes_ =
          Base64::encode(serialized_str.c_str(), serialized_str.size());
    }
  }

  const std::string& attributes() const { return attributes_; }
};

typedef std::shared_ptr<Config> ConfigPtr;

class ForwardAttributeFilter : public Http::StreamDecoderFilter {
 private:
  ConfigPtr config_;

 public:
  ForwardAttributeFilter(ConfigPtr config) : config_(config) {}

  FilterHeadersStatus decodeHeaders(HeaderMap& headers,
                                    bool end_stream) override {
    if (!config_->attributes().empty()) {
      headers.addStatic(Utils::kIstioAttributeHeader, config_->attributes());
    }
    return FilterHeadersStatus::Continue;
  }

  FilterDataStatus decodeData(Buffer::Instance& data,
                              bool end_stream) override {
    return FilterDataStatus::Continue;
  }

  FilterTrailersStatus decodeTrailers(HeaderMap& trailers) override {
    return FilterTrailersStatus::Continue;
  }

  void setDecoderFilterCallbacks(
      StreamDecoderFilterCallbacks& callbacks) override {}
};

}  // namespace ForwardAttribute
}  // namespace Http

namespace Server {
namespace Configuration {

class ForwardAttributeConfig : public HttpFilterConfigFactory {
 public:
  HttpFilterFactoryCb tryCreateFilterFactory(
      HttpFilterType type, const std::string& name, const Json::Object& config,
      const std::string&, Server::Instance& server) override {
    if (type != HttpFilterType::Decoder || name != "forward_attribute") {
      return nullptr;
    }

    Http::ForwardAttribute::ConfigPtr add_header_config(
        new Http::ForwardAttribute::Config(config));
    return [add_header_config](
               Http::FilterChainFactoryCallbacks& callbacks) -> void {
      std::shared_ptr<Http::ForwardAttribute::ForwardAttributeFilter> instance(
          new Http::ForwardAttribute::ForwardAttributeFilter(
              add_header_config));
      callbacks.addStreamDecoderFilter(Http::StreamDecoderFilterPtr(instance));
    };
  }
};

static RegisterHttpFilterConfigFactory<ForwardAttributeConfig> register_;

}  // namespace Configuration
}  // namespace server
