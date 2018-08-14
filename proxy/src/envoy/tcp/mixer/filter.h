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

#pragma once

#include "common/common/logger.h"
#include "envoy/network/connection.h"
#include "envoy/network/filter.h"
#include "proxy/src/envoy/tcp/mixer/control.h"

namespace Envoy {
namespace Tcp {
namespace Mixer {

class Filter : public Network::Filter,
               public Network::ConnectionCallbacks,
               public ::istio::control::tcp::CheckData,
               public ::istio::control::tcp::ReportData,
               public Logger::Loggable<Logger::Id::filter> {
 public:
  Filter(Control& control);
  ~Filter();

  void initializeReadFilterCallbacks(
      Network::ReadFilterCallbacks& callbacks) override;

  // Network::ReadFilter
  Network::FilterStatus onData(Buffer::Instance& data, bool) override;

  // Network::WriteFilter
  Network::FilterStatus onWrite(Buffer::Instance& data, bool) override;
  Network::FilterStatus onNewConnection() override;

  // Network::ConnectionCallbacks
  void onEvent(Network::ConnectionEvent event) override;

  void onAboveWriteBufferHighWatermark() override {}
  void onBelowWriteBufferLowWatermark() override {}

  // CheckData virtual functions.
  bool GetSourceIpPort(std::string* str_ip, int* port) const override;
  bool GetPrincipal(bool peer, std::string* user) const override;
  bool IsMutualTLS() const override;
  bool GetRequestedServerName(std::string* name) const override;

  // ReportData virtual functions.
  bool GetDestinationIpPort(std::string* str_ip, int* port) const override;
  bool GetDestinationUID(std::string* uid) const override;
  void GetReportInfo(
      ::istio::control::tcp::ReportData::ReportInfo* data) const override;
  std::string GetConnectionId() const override;

 private:
  enum class State { NotStarted, Calling, Completed, Closed };
  // This function is invoked when timer event fires.
  // It sends periodical delta reports.
  void OnReportTimer();

  // Makes a Check() call to Mixer.
  void callCheck();

  // Called when Check is done.
  void completeCheck(const ::google::protobuf::util::Status& status);

  // Cancel the pending Check call.
  void cancelCheck();

  // the cancel check
  istio::mixerclient::CancelFunc cancel_check_;
  // the control object.
  Control& control_;
  // pre-request handler
  std::unique_ptr<::istio::control::tcp::RequestHandler> handler_;
  // filter callback
  Network::ReadFilterCallbacks* filter_callbacks_{};
  // state
  State state_{State::NotStarted};
  // calling_check
  bool calling_check_{};
  // received bytes
  uint64_t received_bytes_{};
  // send bytes
  uint64_t send_bytes_{};

  // Timer that periodically sends reports.
  Event::TimerPtr report_timer_;
  // start_time
  std::chrono::time_point<std::chrono::system_clock> start_time_;
};

}  // namespace Mixer
}  // namespace Tcp
}  // namespace Envoy
