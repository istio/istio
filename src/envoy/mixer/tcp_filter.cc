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

#include "common/common/enum_to_int.h"
#include "common/common/logger.h"
#include "envoy/network/connection.h"
#include "envoy/network/filter.h"
#include "envoy/registry/registry.h"
#include "server/config/network/http_connection_manager.h"
#include "src/envoy/mixer/config.h"
#include "src/envoy/mixer/mixer_control.h"
#include "src/envoy/mixer/thread_dispatcher.h"

using ::google::protobuf::util::Status;
using StatusCode = ::google::protobuf::util::error::Code;

namespace Envoy {
namespace Http {
namespace Mixer {

class TcpConfig : public Logger::Loggable<Logger::Id::filter> {
 private:
  Upstream::ClusterManager& cm_;
  MixerConfig mixer_config_;
  MixerControlPerThreadStore mixer_control_store_;

 public:
  TcpConfig(const Json::Object& config,
            Server::Configuration::FactoryContext& context)
      : cm_(context.clusterManager()),
        mixer_control_store_([this]() -> std::shared_ptr<MixerControl> {
          return std::make_shared<MixerControl>(mixer_config_, cm_);
        }) {
    mixer_config_.Load(config);
    log().debug("Mixer Tcp Filter Config loaded.");
  }

  std::shared_ptr<MixerControl> mixer_control() {
    return mixer_control_store_.Get();
  }
};

typedef std::shared_ptr<TcpConfig> TcpConfigPtr;

class TcpInstance : public Network::Filter,
                    public Network::ConnectionCallbacks,
                    public Logger::Loggable<Logger::Id::filter>,
                    public std::enable_shared_from_this<TcpInstance> {
 private:
  enum class State { NotStarted, Calling, Completed, Closed };

  TcpConfigPtr config_;
  std::shared_ptr<MixerControl> mixer_control_;
  std::shared_ptr<HttpRequestData> request_data_;
  Network::ReadFilterCallbacks* filter_callbacks_{};
  State state_{State::NotStarted};
  bool calling_check_{};
  uint64_t received_bytes_{};
  uint64_t send_bytes_{};
  int check_status_code_{};
  std::chrono::time_point<std::chrono::system_clock> start_time_;

 public:
  TcpInstance(TcpConfigPtr config)
      : config_(config), mixer_control_(config->mixer_control()) {
    log().debug("Called TcpInstance: {}", __func__);
  }
  ~TcpInstance() { log().debug("Called TcpInstance : {}", __func__); }

  // Returns a shared pointer of this object.
  std::shared_ptr<TcpInstance> GetPtr() { return shared_from_this(); }

  void initializeReadFilterCallbacks(
      Network::ReadFilterCallbacks& callbacks) override {
    log().debug("Called TcpInstance: {}", __func__);
    filter_callbacks_ = &callbacks;
    filter_callbacks_->connection().addConnectionCallbacks(*this);
    SetThreadDispatcher(filter_callbacks_->connection().dispatcher());
    start_time_ = std::chrono::system_clock::now();
  }

  // Network::ReadFilter
  Network::FilterStatus onData(Buffer::Instance& data) override {
    conn_log_debug("Called TcpInstance onRead bytes: {}",
                   filter_callbacks_->connection(), data.length());
    received_bytes_ += data.length();
    return Network::FilterStatus::Continue;
  }

  // Network::WriteFilter
  Network::FilterStatus onWrite(Buffer::Instance& data) override {
    conn_log_debug("Called TcpInstance onWrite bytes: {}",
                   filter_callbacks_->connection(), data.length());
    send_bytes_ += data.length();
    return Network::FilterStatus::Continue;
  }

  Network::FilterStatus onNewConnection() override {
    conn_log_debug("Called TcpInstance onNewConnection: remote {}, local {}",
                   filter_callbacks_->connection(),
                   filter_callbacks_->connection().remoteAddress().asString(),
                   filter_callbacks_->connection().localAddress().asString());

    if (state_ == State::NotStarted) {
      state_ = State::Calling;
      request_data_ = std::make_shared<HttpRequestData>();

      std::string origin_user;
      Ssl::Connection* ssl = filter_callbacks_->connection().ssl();
      if (ssl != nullptr) {
        origin_user = ssl->uriSanPeerCertificate();
      }

      filter_callbacks_->connection().readDisable(true);
      auto instance = GetPtr();
      calling_check_ = true;
      mixer_control_->CheckTcp(request_data_, filter_callbacks_->connection(),
                               origin_user, [instance](const Status& status) {
                                 instance->completeCheck(status);
                               });
      calling_check_ = false;
    }
    return state_ == State::Calling ? Network::FilterStatus::StopIteration
                                    : Network::FilterStatus::Continue;
  }

  void completeCheck(const Status& status) {
    log().debug("Called TcpInstance completeCheck: {}", status.ToString());
    if (state_ == State::Closed) {
      return;
    }
    state_ = State::Completed;
    filter_callbacks_->connection().readDisable(false);

    if (!status.ok()) {
      check_status_code_ = status.error_code();
      filter_callbacks_->connection().close(
          Network::ConnectionCloseType::NoFlush);
    } else {
      if (!calling_check_) {
        filter_callbacks_->continueReading();
      }
    }
  }

  // Network::ConnectionCallbacks
  void onEvent(Network::ConnectionEvent event) override {
    if (filter_callbacks_->upstreamHost()) {
      log().debug("Called TcpInstance onEvent: {} upstream {}",
                  enumToInt(event),
                  filter_callbacks_->upstreamHost()->address()->asString());
    } else {
      log().debug("Called TcpInstance onEvent: {}", enumToInt(event));
    }

    if (event == Network::ConnectionEvent::RemoteClose ||
        event == Network::ConnectionEvent::LocalClose) {
      if (state_ != State::Closed && request_data_) {
        mixer_control_->ReportTcp(
            request_data_, received_bytes_, send_bytes_, check_status_code_,
            std::chrono::duration_cast<std::chrono::nanoseconds>(
                std::chrono::system_clock::now() - start_time_),
            filter_callbacks_->upstreamHost());
      }
      state_ = State::Closed;
    }
  }

  void onAboveWriteBufferHighWatermark() override {}
  void onBelowWriteBufferLowWatermark() override {}
};

}  // namespace Mixer
}  // namespace Http

namespace Server {
namespace Configuration {

class TcpMixerFilterFactory : public NamedNetworkFilterConfigFactory {
 public:
  NetworkFilterFactoryCb createFilterFactory(const Json::Object& config,
                                             FactoryContext& context) {
    Http::Mixer::TcpConfigPtr tcp_config(
        new Http::Mixer::TcpConfig(config, context));
    return [tcp_config](Network::FilterManager& filter_manager) -> void {
      std::shared_ptr<Http::Mixer::TcpInstance> instance =
          std::make_shared<Http::Mixer::TcpInstance>(tcp_config);
      filter_manager.addReadFilter(Network::ReadFilterSharedPtr(instance));
      filter_manager.addWriteFilter(Network::WriteFilterSharedPtr(instance));
    };
  }
  std::string name() override { return "mixer"; }
  NetworkFilterType type() override { return NetworkFilterType::Both; }
};

static Registry::RegisterFactory<TcpMixerFilterFactory,
                                 NamedNetworkFilterConfigFactory>
    register_;

}  // namespace Configuration
}  // namespace Server
}  // namespace Envoy
