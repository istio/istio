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

#include "common/common/base64.h"
#include "common/common/logger.h"
#include "common/http/headers.h"
#include "common/http/utility.h"
#include "envoy/server/instance.h"
#include "envoy/ssl/connection.h"
#include "server/config/network/http_connection_manager.h"
#include "src/envoy/mixer/config.h"
#include "src/envoy/mixer/http_control.h"
#include "src/envoy/mixer/utils.h"

#include <map>
#include <mutex>
#include <thread>

using ::google::protobuf::util::Status;
using StatusCode = ::google::protobuf::util::error::Code;
using ::istio::mixer_client::DoneFunc;

namespace Envoy {
namespace Http {
namespace Mixer {
namespace {

// Switch to turn off attribute forwarding
const std::string kJsonNameForwardSwitch("mixer_forward");

// Switch to turn off mixer check/report/quota
const std::string kJsonNameMixerSwitch("mixer_control");

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
  Upstream::ClusterManager& cm_;
  std::string forward_attributes_;
  MixerConfig mixer_config_;
  std::mutex map_mutex_;
  std::map<std::thread::id, std::shared_ptr<HttpControl>> http_control_map_;

 public:
  Config(const Json::Object& config, Server::Instance& server)
      : cm_(server.clusterManager()) {
    mixer_config_.Load(config);
    if (mixer_config_.mixer_server.empty()) {
      log().error(
          "mixer_server is required but not specified in the config: {}",
          __func__);
    } else {
      log().debug("Called Mixer::Config constructor with mixer_server: ",
                  mixer_config_.mixer_server);
    }

    if (!mixer_config_.forward_attributes.empty()) {
      std::string serialized_str =
          Utils::SerializeStringMap(mixer_config_.forward_attributes);
      forward_attributes_ =
          Base64::encode(serialized_str.c_str(), serialized_str.size());
      log().debug("Mixer forward attributes set: ", serialized_str);
    }
  }

  std::shared_ptr<HttpControl> http_control() {
    std::thread::id id = std::this_thread::get_id();
    std::lock_guard<std::mutex> lock(map_mutex_);
    auto it = http_control_map_.find(id);
    if (it != http_control_map_.end()) {
      return it->second;
    }
    auto http_control = std::make_shared<HttpControl>(mixer_config_);
    http_control_map_[id] = http_control;
    return http_control;
  }
  const std::string& forward_attributes() const { return forward_attributes_; }
};

typedef std::shared_ptr<Config> ConfigPtr;

class Instance : public Http::StreamDecoderFilter,
                 public Http::AccessLog::Instance,
                 public std::enable_shared_from_this<Instance> {
 private:
  std::shared_ptr<HttpControl> http_control_;
  ConfigPtr config_;
  std::shared_ptr<HttpRequestData> request_data_;

  enum State { NotStarted, Calling, Complete, Responded };
  State state_;

  StreamDecoderFilterCallbacks* decoder_callbacks_;

  bool initiating_call_;
  int check_status_code_;

  bool mixer_disabled_;

  // mixer control switch (off by default)
  bool mixer_disabled() {
    auto route = decoder_callbacks_->route();
    if (route != nullptr) {
      auto entry = route->routeEntry();
      if (entry != nullptr) {
        auto key = entry->opaqueConfig().find(kJsonNameMixerSwitch);
        if (key != entry->opaqueConfig().end() && key->second == "on") {
          return false;
        }
      }
    }
    return true;
  }

  // attribute forward switch (on by default)
  bool forward_disabled() {
    auto route = decoder_callbacks_->route();
    if (route != nullptr) {
      auto entry = route->routeEntry();
      if (entry != nullptr) {
        auto key = entry->opaqueConfig().find(kJsonNameForwardSwitch);
        if (key != entry->opaqueConfig().end() && key->second == "off") {
          return true;
        }
      }
    }
    return false;
  }

 public:
  Instance(ConfigPtr config)
      : http_control_(config->http_control()),
        config_(config),
        state_(NotStarted),
        initiating_call_(false),
        check_status_code_(HttpCode(StatusCode::UNKNOWN)) {
    Log().debug("Called Mixer::Instance : {}", __func__);
  }

  // Returns a shared pointer of this object.
  std::shared_ptr<Instance> GetPtr() { return shared_from_this(); }

  // Jump thread; on_done will be called at the dispatcher thread.
  DoneFunc GetThreadJumpFunc(DoneFunc on_done) {
    auto& dispatcher = decoder_callbacks_->dispatcher();
    return [&dispatcher, on_done](const Status& status) {
      dispatcher.post([status, on_done]() { on_done(status); });
    };
  }

  FilterHeadersStatus decodeHeaders(HeaderMap& headers,
                                    bool end_stream) override {
    Log().debug("Called Mixer::Instance : {}", __func__);

    if (!config_->forward_attributes().empty() && !forward_disabled()) {
      headers.addStatic(Utils::kIstioAttributeHeader,
                        config_->forward_attributes());
    }

    mixer_disabled_ = mixer_disabled();
    if (mixer_disabled_) {
      return FilterHeadersStatus::Continue;
    }

    state_ = Calling;
    initiating_call_ = true;
    request_data_ = std::make_shared<HttpRequestData>();

    std::string origin_user;
    Ssl::Connection* ssl =
        const_cast<Ssl::Connection*>(decoder_callbacks_->ssl());
    if (ssl != nullptr) {
      origin_user = ssl->uriSanPeerCertificate();
    }

    auto instance = GetPtr();
    http_control_->Check(request_data_, headers, origin_user,
                         GetThreadJumpFunc([instance](const Status& status) {
                           instance->callQuota(status);
                         }));
    initiating_call_ = false;

    if (state_ == Complete) {
      return FilterHeadersStatus::Continue;
    }
    Log().debug("Called Mixer::Instance : {} Stop", __func__);
    return FilterHeadersStatus::StopIteration;
  }

  FilterDataStatus decodeData(Buffer::Instance& data,
                              bool end_stream) override {
    if (mixer_disabled_) {
      return FilterDataStatus::Continue;
    }

    Log().debug("Called Mixer::Instance : {} ({}, {})", __func__, data.length(),
                end_stream);
    if (state_ == Calling) {
      return FilterDataStatus::StopIterationAndBuffer;
    }
    return FilterDataStatus::Continue;
  }

  FilterTrailersStatus decodeTrailers(HeaderMap& trailers) override {
    if (mixer_disabled_) {
      return FilterTrailersStatus::Continue;
    }

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
  }

  void callQuota(const Status& status) {
    // This stream has been reset, abort the callback.
    if (state_ == Responded) {
      return;
    }
    if (!status.ok()) {
      completeCheck(status);
      return;
    }
    auto instance = GetPtr();
    http_control_->Quota(request_data_,
                         GetThreadJumpFunc([instance](const Status& status) {
                           instance->completeCheck(status);
                         }));
  }

  void completeCheck(const Status& status) {
    Log().debug("Called Mixer::Instance : check complete {}",
                status.ToString());
    // This stream has been reset, abort the callback.
    if (state_ == Responded) {
      return;
    }
    if (!status.ok() && state_ != Responded) {
      state_ = Responded;
      check_status_code_ = HttpCode(status.error_code());
      Utility::sendLocalReply(*decoder_callbacks_, Code(check_status_code_),
                              status.ToString());
      return;
    }

    state_ = Complete;
    if (!initiating_call_) {
      decoder_callbacks_->continueDecoding();
    }
  }

  void onDestroy() override { state_ = Responded; }

  virtual void log(const HeaderMap* request_headers,
                   const HeaderMap* response_headers,
                   const AccessLog::RequestInfo& request_info) override {
    Log().debug("Called Mixer::Instance : {}", __func__);
    // If decodeHaeders() is not called, not to call Mixer report.
    if (!request_data_) return;
    // Make sure not to use any class members at the callback.
    // The class may be gone when it is called.
    // Log() is a static function so it is OK.
    http_control_->Report(request_data_, response_headers, request_info,
                          check_status_code_, [](const Status& status) {
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
    if (type != HttpFilterType::Decoder || name != "mixer") {
      return nullptr;
    }

    Http::Mixer::ConfigPtr mixer_config(
        new Http::Mixer::Config(config, server));
    return
        [mixer_config](Http::FilterChainFactoryCallbacks& callbacks) -> void {
          std::shared_ptr<Http::Mixer::Instance> instance =
              std::make_shared<Http::Mixer::Instance>(mixer_config);
          callbacks.addStreamDecoderFilter(
              Http::StreamDecoderFilterSharedPtr(instance));
          callbacks.addAccessLogHandler(
              Http::AccessLog::InstanceSharedPtr(instance));
        };
  }
};

static RegisterHttpFilterConfigFactory<MixerConfig> register_;

}  // namespace Configuration
}  // namespace Server
}  // namespace Envoy
