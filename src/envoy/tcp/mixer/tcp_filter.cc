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
#include "src/envoy/tcp/mixer/config.h"
#include "src/envoy/tcp/mixer/mixer_control.h"
#include "src/envoy/utils/stats.h"
#include "src/envoy/utils/utils.h"

using ::google::protobuf::util::Status;
using StatusCode = ::google::protobuf::util::error::Code;
using ::istio::mixerclient::Statistics;

namespace Envoy {
namespace Tcp {
namespace Mixer {
namespace {

// Envoy stats perfix for TCP filter stats.
const std::string kTcpStatsPrefix("tcp_mixer_filter.");

}  // namespace

class TcpConfig : public Logger::Loggable<Logger::Id::filter> {
 private:
  static Utils::MixerFilterStats generateStats(const std::string& name,
                                               Stats::Scope& scope) {
    return {ALL_MIXER_FILTER_STATS(POOL_COUNTER_PREFIX(scope, name))};
  }

  Upstream::ClusterManager& cm_;
  TcpMixerConfig mixer_config_;
  ThreadLocal::SlotPtr tls_;
  Utils::MixerFilterStats stats_;

 public:
  TcpConfig(const Json::Object& config,
            Server::Configuration::FactoryContext& context)
      : cm_(context.clusterManager()),
        tls_(context.threadLocal().allocateSlot()),
        stats_(generateStats(kTcpStatsPrefix, context.scope())) {
    mixer_config_.Load(config);
    Runtime::RandomGenerator& random = context.random();
    tls_->set([this, &random](Event::Dispatcher& dispatcher)
                  -> ThreadLocal::ThreadLocalObjectSharedPtr {
                    return ThreadLocal::ThreadLocalObjectSharedPtr(
                        new TcpMixerControl(mixer_config_, cm_, dispatcher,
                                            random, stats_));
                  });
  }

  TcpMixerControl& mixer_control() { return tls_->getTyped<TcpMixerControl>(); }
};

typedef std::shared_ptr<TcpConfig> TcpConfigPtr;

class TcpInstance : public Network::Filter,
                    public Network::ConnectionCallbacks,
                    public ::istio::control::tcp::CheckData,
                    public ::istio::control::tcp::ReportData,
                    public Logger::Loggable<Logger::Id::filter> {
 private:
  enum class State { NotStarted, Calling, Completed, Closed };

  // This function is invoked when timer event fires. It sends periodical delta
  // reports.
  void OnTimer() {
    handler_->Report(this, /* is_final_report */ false);
    report_timer_->enableTimer(mixer_control_.report_interval_ms());
  }

  istio::mixerclient::CancelFunc cancel_check_;
  TcpMixerControl& mixer_control_;
  std::unique_ptr<::istio::control::tcp::RequestHandler> handler_;
  Network::ReadFilterCallbacks* filter_callbacks_{};
  State state_{State::NotStarted};
  bool calling_check_{};
  uint64_t received_bytes_{};
  uint64_t send_bytes_{};
  int check_status_code_{};
  std::chrono::time_point<std::chrono::system_clock> start_time_;

  // Timer that periodically sends reports.
  Event::TimerPtr report_timer_;

 public:
  TcpInstance(TcpConfigPtr config) : mixer_control_(config->mixer_control()) {
    ENVOY_LOG(debug, "Called TcpInstance: {}", __func__);
  }

  void cancelCheck() {
    if (state_ != State::Calling) {
      cancel_check_ = nullptr;
    }
    state_ = State::Closed;
    if (cancel_check_) {
      ENVOY_LOG(debug, "Cancelling check call");
      cancel_check_();
      cancel_check_ = nullptr;
    }
  }

  ~TcpInstance() {
    cancelCheck();
    ENVOY_LOG(debug, "Called TcpInstance : {}", __func__);
  }

  void initializeReadFilterCallbacks(
      Network::ReadFilterCallbacks& callbacks) override {
    ENVOY_LOG(debug, "Called TcpInstance: {}", __func__);
    filter_callbacks_ = &callbacks;
    filter_callbacks_->connection().addConnectionCallbacks(*this);
    start_time_ = std::chrono::system_clock::now();
  }

  // Makes a Check() call to Mixer.
  void callCheck() {
    handler_ = mixer_control_.controller()->CreateRequestHandler();

    state_ = State::Calling;
    filter_callbacks_->connection().readDisable(true);
    calling_check_ = true;
    cancel_check_ = handler_->Check(
        this, [this](const Status& status) { completeCheck(status); });
    calling_check_ = false;
  }

  // Network::ReadFilter
  Network::FilterStatus onData(Buffer::Instance& data, bool) override {
    if (state_ == State::NotStarted) {
      // By waiting to invoke the callCheck() at onData(), the call to Mixer
      // will have sufficient SSL information to fill the check Request.
      callCheck();
    }

    ENVOY_CONN_LOG(debug, "Called TcpInstance onRead bytes: {}",
                   filter_callbacks_->connection(), data.length());
    received_bytes_ += data.length();

    return state_ == State::Calling ? Network::FilterStatus::StopIteration
                                    : Network::FilterStatus::Continue;
  }

  // Network::WriteFilter
  Network::FilterStatus onWrite(Buffer::Instance& data, bool) override {
    ENVOY_CONN_LOG(debug, "Called TcpInstance onWrite bytes: {}",
                   filter_callbacks_->connection(), data.length());
    send_bytes_ += data.length();
    return Network::FilterStatus::Continue;
  }

  Network::FilterStatus onNewConnection() override {
    ENVOY_CONN_LOG(debug,
                   "Called TcpInstance onNewConnection: remote {}, local {}",
                   filter_callbacks_->connection(),
                   filter_callbacks_->connection().remoteAddress()->asString(),
                   filter_callbacks_->connection().localAddress()->asString());

    // Wait until onData() is invoked.
    return Network::FilterStatus::Continue;
  }

  void completeCheck(const Status& status) {
    ENVOY_LOG(debug, "Called TcpInstance completeCheck: {}", status.ToString());
    cancel_check_ = nullptr;
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
      report_timer_ =
          mixer_control_.dispatcher().createTimer([this]() { OnTimer(); });
      report_timer_->enableTimer(mixer_control_.report_interval_ms());
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
      if (state_ != State::Closed && handler_) {
        if (report_timer_) {
          report_timer_->disableTimer();
        }
        handler_->Report(this, /* is_final_report */ true);
      }
      cancelCheck();
    }
  }

  void onAboveWriteBufferHighWatermark() override {}
  void onBelowWriteBufferLowWatermark() override {}

  bool GetSourceIpPort(std::string* str_ip, int* port) const override {
    return Utils::GetIpPort(
        filter_callbacks_->connection().remoteAddress()->ip(), str_ip, port);
  }
  bool GetSourceUser(std::string* user) const override {
    return Utils::GetSourceUser(&filter_callbacks_->connection(), user);
  }

  bool IsMutualTLS() const override {
    return Utils::IsMutualTLS(&filter_callbacks_->connection());
  }

  bool GetDestinationIpPort(std::string* str_ip, int* port) const override {
    if (filter_callbacks_->upstreamHost() &&
        filter_callbacks_->upstreamHost()->address()) {
      return Utils::GetIpPort(
          filter_callbacks_->upstreamHost()->address()->ip(), str_ip, port);
    }
    return false;
  }
  void GetReportInfo(
      ::istio::control::tcp::ReportData::ReportInfo* data) const override {
    data->received_bytes = received_bytes_;
    data->send_bytes = send_bytes_;
    data->duration = std::chrono::duration_cast<std::chrono::nanoseconds>(
        std::chrono::system_clock::now() - start_time_);
  }
};

}  // namespace Mixer
}  // namespace Tcp

namespace Server {
namespace Configuration {

class TcpMixerFilterFactory : public NamedNetworkFilterConfigFactory {
 public:
  NetworkFilterFactoryCb createFilterFactory(const Json::Object& config,
                                             FactoryContext& context) override {
    Tcp::Mixer::TcpConfigPtr tcp_config(
        new Tcp::Mixer::TcpConfig(config, context));
    return [tcp_config](Network::FilterManager& filter_manager) -> void {
      std::shared_ptr<Tcp::Mixer::TcpInstance> instance =
          std::make_shared<Tcp::Mixer::TcpInstance>(tcp_config);
      filter_manager.addReadFilter(Network::ReadFilterSharedPtr(instance));
      filter_manager.addWriteFilter(Network::WriteFilterSharedPtr(instance));
    };
  }
  std::string name() override { return "mixer"; }
};

static Registry::RegisterFactory<TcpMixerFilterFactory,
                                 NamedNetworkFilterConfigFactory>
    register_;

}  // namespace Configuration
}  // namespace Server
}  // namespace Envoy
