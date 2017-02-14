/* Copyright 2016 Google Inc. All Rights Reserved.
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

#include "common/common/logger.h"
#include "common/http/headers.h"
#include "common/http/utility.h"
#include "envoy/server/instance.h"
#include "server/config/network/http_connection_manager.h"
#include "src/envoy/mixer/http_control.h"

using ::google::protobuf::util::Status;
using StatusCode = ::google::protobuf::util::error::Code;
using ::istio::mixer_client::DoneFunc;

namespace Http {
namespace Mixer {
namespace {

// Define lower case string for X-Forwarded-Host.
const LowerCaseString kHeaderNameXFH("x-forwarded-host", false);

// Convert Status::code to HTTP code
int HttpCode(int code) {
  // Map Canonical codes to HTTP status codes. This is based on the mapping
  // defined by the protobuf http error space.
  switch (code) {
    case StatusCode::OK:
      return 200;
    case StatusCode::CANCELLED:
      return 499;
    case StatusCode::UNKNOWN:
      return 500;
    case StatusCode::INVALID_ARGUMENT:
      return 400;
    case StatusCode::DEADLINE_EXCEEDED:
      return 504;
    case StatusCode::NOT_FOUND:
      return 404;
    case StatusCode::ALREADY_EXISTS:
      return 409;
    case StatusCode::PERMISSION_DENIED:
      return 403;
    case StatusCode::RESOURCE_EXHAUSTED:
      return 429;
    case StatusCode::FAILED_PRECONDITION:
      return 400;
    case StatusCode::ABORTED:
      return 409;
    case StatusCode::OUT_OF_RANGE:
      return 400;
    case StatusCode::UNIMPLEMENTED:
      return 501;
    case StatusCode::INTERNAL:
      return 500;
    case StatusCode::UNAVAILABLE:
      return 503;
    case StatusCode::DATA_LOSS:
      return 500;
    case StatusCode::UNAUTHENTICATED:
      return 401;
    default:
      return 500;
  }
}

}  // namespace

class Config : public Logger::Loggable<Logger::Id::http> {
 private:
  std::shared_ptr<HttpControl> http_control_;
  Upstream::ClusterManager& cm_;

 public:
  Config(const Json::Object& config, Server::Instance& server)
      : cm_(server.clusterManager()) {
    std::string mixer_server;
    if (config.hasObject("mixer_server")) {
      mixer_server = config.getString("mixer_server");
    } else {
      log().error(
          "mixer_server is required but not specified in the config: {}",
          __func__);
    }

    std::map<std::string, std::string> attributes;
    if (config.hasObject("attributes")) {
      for (const std::string& attr : config.getStringArray("attributes")) {
        attributes[attr] = config.getString(attr);
      }
    }

    http_control_ =
        std::make_shared<HttpControl>(mixer_server, std::move(attributes));
    log().debug("Called Mixer::Config contructor with mixer_server: ",
                mixer_server);
  }

  std::shared_ptr<HttpControl>& http_control() { return http_control_; }
};

typedef std::shared_ptr<Config> ConfigPtr;

class Instance : public Http::StreamFilter, public Http::AccessLog::Instance {
 private:
  std::shared_ptr<HttpControl> http_control_;
  std::shared_ptr<HttpRequestData> request_data_;

  enum State { NotStarted, Calling, Complete, Responded };
  State state_;

  StreamDecoderFilterCallbacks* decoder_callbacks_;
  StreamEncoderFilterCallbacks* encoder_callbacks_;

  bool initiating_call_;

 public:
  Instance(ConfigPtr config)
      : http_control_(config->http_control()),
        state_(NotStarted),
        initiating_call_(false) {
    Log().debug("Called Mixer::Instance : {}", __func__);
  }

  // Jump thread; on_done will be called at the dispatcher thread.
  DoneFunc wrapper(DoneFunc on_done) {
    auto& dispatcher = decoder_callbacks_->dispatcher();
    return [&dispatcher, on_done](const Status& status) {
      dispatcher.post([status, on_done]() { on_done(status); });
    };
  }

  FilterHeadersStatus decodeHeaders(HeaderMap& headers,
                                    bool end_stream) override {
    Log().debug("Called Mixer::Instance : {}", __func__);
    state_ = Calling;
    initiating_call_ = true;
    request_data_ = std::make_shared<HttpRequestData>();
    http_control_->Check(
        request_data_, headers,
        wrapper([this](const Status& status) { completeCheck(status); }));
    initiating_call_ = false;

    if (state_ == Complete) {
      return FilterHeadersStatus::Continue;
    }
    Log().debug("Called Mixer::Instance : {} Stop", __func__);
    return FilterHeadersStatus::StopIteration;
  }

  FilterDataStatus decodeData(Buffer::Instance& data,
                              bool end_stream) override {
    Log().debug("Called Mixer::Instance : {} ({}, {})", __func__, data.length(),
                end_stream);
    if (state_ == Calling) {
      return FilterDataStatus::StopIterationAndBuffer;
    }
    return FilterDataStatus::Continue;
  }

  FilterTrailersStatus decodeTrailers(HeaderMap& trailers) override {
    Log().debug("Called Mixer::Instance : {}", __func__);
    if (state_ == Calling) {
      return FilterTrailersStatus::StopIteration;
    }
    return FilterTrailersStatus::Continue;
  }
  void setDecoderFilterCallbacks(
      StreamDecoderFilterCallbacks& callbacks) override {
    Log().debug("Called Mixer::Instance : {}", __func__);
    decoder_callbacks_ = &callbacks;
    decoder_callbacks_->addResetStreamCallback(
        [this]() { state_ = Responded; });
  }
  void completeCheck(const Status& status) {
    Log().debug("Called Mixer::Instance : check complete {}",
                status.ToString());
    if (!status.ok() && state_ != Responded) {
      state_ = Responded;
      Utility::sendLocalReply(*decoder_callbacks_,
                              Code(HttpCode(status.error_code())),
                              status.ToString());
      return;
    }
    state_ = Complete;
    if (!initiating_call_) {
      decoder_callbacks_->continueDecoding();
    }
  }

  virtual FilterHeadersStatus encodeHeaders(HeaderMap& headers,
                                            bool end_stream) override {
    Log().debug("Called Mixer::Instance : {}", __func__);
    return FilterHeadersStatus::Continue;
  }
  virtual FilterDataStatus encodeData(Buffer::Instance& data,
                                      bool end_stream) override {
    Log().debug("Called Mixer::Instance : {}", __func__);
    return FilterDataStatus::Continue;
  }
  virtual FilterTrailersStatus encodeTrailers(HeaderMap& trailers) override {
    Log().debug("Called Mixer::Instance : {}", __func__);
    return FilterTrailersStatus::Continue;
  }
  virtual void setEncoderFilterCallbacks(
      StreamEncoderFilterCallbacks& callbacks) override {
    Log().debug("Called Mixer::Instance : {}", __func__);
    encoder_callbacks_ = &callbacks;
  }

  virtual void log(const HeaderMap* request_headers,
                   const HeaderMap* response_headers,
                   const AccessLog::RequestInfo& request_info) override {
    Log().debug("Called Mixer::Instance : {}", __func__);
    // Make sure not to use any class members at the callback.
    // The class may be gone when it is called.
    // Log() is a static function so it is OK.
    http_control_->Report(request_data_, response_headers, request_info,
                          [](const Status& status) {
                            Log().debug("Report returns status: {}",
                                        status.ToString());
                          });
  }

  static spdlog::logger& Log() {
    static spdlog::logger& instance =
        Logger::Registry::getLog(Logger::Id::http);
    return instance;
  }
};

}  // namespace Mixer
}  // namespace Http

namespace Server {
namespace Configuration {

class MixerConfig : public HttpFilterConfigFactory {
 public:
  HttpFilterFactoryCb tryCreateFilterFactory(
      HttpFilterType type, const std::string& name, const Json::Object& config,
      const std::string&, Server::Instance& server) override {
    if (type != HttpFilterType::Both || name != "mixer") {
      return nullptr;
    }

    Http::Mixer::ConfigPtr mixer_config(
        new Http::Mixer::Config(config, server));
    return
        [mixer_config](Http::FilterChainFactoryCallbacks& callbacks) -> void {
          std::shared_ptr<Http::Mixer::Instance> instance(
              new Http::Mixer::Instance(mixer_config));
          callbacks.addStreamFilter(Http::StreamFilterPtr(instance));
          callbacks.addAccessLogHandler(Http::AccessLog::InstancePtr(instance));
        };
  }
};

static RegisterHttpFilterConfigFactory<MixerConfig> register_;

}  // namespace Configuration
}  // namespace server
