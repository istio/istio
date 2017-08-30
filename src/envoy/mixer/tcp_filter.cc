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
#include "envoy/server/instance.h"
#include "server/config/network/http_connection_manager.h"
#include "src/envoy/mixer/config.h"
#include "src/envoy/mixer/mixer_control.h"

using ::google::protobuf::util::Status;
using StatusCode = ::google::protobuf::util::error::Code;

namespace Envoy {
namespace Http {
namespace Mixer {

class TcpConfig : public Logger::Loggable<Logger::Id::filter> {
 private:
  Upstream::ClusterManager& cm_;
  MixerConfig mixer_config_;
  ThreadLocal::SlotPtr tls_;

 public:
  TcpConfig(const Json::Object& config,
            Server::Configuration::FactoryContext& context)
      : cm_(context.clusterManager()),
        tls_(context.threadLocal().allocateSlot()) {
    mixer_config_.Load(config);
    Runtime::RandomGenerator& random = context.random();
    tls_->set(
        [this, &random](Event::Dispatcher& dispatcher)
            -> ThreadLocal::ThreadLocalObjectSharedPtr {
              return ThreadLocal::ThreadLocalObjectSharedPtr(
                  new MixerControl(mixer_config_, cm_, dispatcher, random));
            });
  }

  MixerControl& mixer_control() { return tls_->getTyped<MixerControl>(); }
};

typedef std::shared_ptr<TcpConfig> TcpConfigPtr;

class TcpInstance : public Network::Filter,
                    public Network::ConnectionCallbacks,
                    public Logger::Loggable<Logger::Id::filter> {
 private:
  enum class State { NotStarted, Calling, Completed, Closed };

  istio::mixer_client::CancelFunc cancel_check_;
  MixerControl& mixer_control_;
  std::shared_ptr<HttpRequestData> request_data_;
  Network::ReadFilterCallbacks* filter_callbacks_{};
  State state_{State::NotStarted};
  bool calling_check_{};
  uint64_t received_bytes_{};
  uint64_t send_bytes_{};
  int check_status_code_{};
  std::chrono::time_point<std::chrono::system_clock> start_time_;

 public:
  TcpInstance(TcpConfigPtr config) : mixer_control_(config->mixer_control()) {
    ENVOY_LOG(debug, "Called TcpInstance: {}", __func__);
  }

  ~TcpInstance() {
    if (state_ != State::Calling) {
      cancel_check_ = nullptr;
    }
    state_ = State::Closed;
    if (cancel_check_) {
      ENVOY_LOG(debug, "Cancelling check call");
      cancel_check_();
    }
    ENVOY_LOG(debug, "Called TcpInstance : {}", __func__);
  }

  void initializeReadFilterCallbacks(
      Network::ReadFilterCallbacks& callbacks) override {
    ENVOY_LOG(debug, "Called TcpInstance: {}", __func__);
    filter_callbacks_ = &callbacks;
    filter_callbacks_->connection().addConnectionCallbacks(*this);
    start_time_ = std::chrono::system_clock::now();
  }

  // Network::ReadFilter
  Network::FilterStatus onData(Buffer::Instance& data) override {
    ENVOY_CONN_LOG(debug, "Called TcpInstance onRead bytes: {}",
                   filter_callbacks_->connection(), data.length());
    received_bytes_ += data.length();
    return Network::FilterStatus::Continue;
  }

  // Network::WriteFilter
  Network::FilterStatus onWrite(Buffer::Instance& data) override {
    ENVOY_CONN_LOG(debug, "Called TcpInstance onWrite bytes: {}",
                   filter_callbacks_->connection(), data.length());
    send_bytes_ += data.length();
    return Network::FilterStatus::Continue;
  }

  Network::FilterStatus onNewConnection() override {
    ENVOY_CONN_LOG(debug,
                   "Called TcpInstance onNewConnection: remote {}, local {}",
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
      calling_check_ = true;
      mixer_control_.BuildTcpCheck(
          request_data_, filter_callbacks_->connection(), origin_user);
      cancel_check_ = mixer_control_.SendCheck(
          request_data_, nullptr,
          [this](const Status& status) { completeCheck(status); });
      calling_check_ = false;
    }
    return state_ == State::Calling ? Network::FilterStatus::StopIteration
                                    : Network::FilterStatus::Continue;
  }

  void completeCheck(const Status& status) {
    ENVOY_LOG(debug, "Called TcpInstance completeCheck: {}", status.ToString());
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
      ENVOY_LOG(debug, "Called TcpInstance onEvent: {} upstream {}",
                enumToInt(event),
                filter_callbacks_->upstreamHost()->address()->asString());
    } else {
      ENVOY_LOG(debug, "Called TcpInstance onEvent: {}", enumToInt(event));
    }

    if (event == Network::ConnectionEvent::RemoteClose ||
        event == Network::ConnectionEvent::LocalClose) {
      if (state_ != State::Closed && request_data_) {
        mixer_control_.BuildTcpReport(
            request_data_, received_bytes_, send_bytes_, check_status_code_,
            std::chrono::duration_cast<std::chrono::nanoseconds>(
                std::chrono::system_clock::now() - start_time_),
            filter_callbacks_->upstreamHost());
        mixer_control_.SendReport(request_data_);
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
                                             FactoryContext& context) override {
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
