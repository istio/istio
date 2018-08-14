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

#include "proxy/src/envoy/tcp/mixer/filter.h"
#include "common/common/enum_to_int.h"
#include "proxy/src/envoy/utils/utils.h"

using ::google::protobuf::util::Status;
using ::istio::mixerclient::CheckResponseInfo;

namespace Envoy {
namespace Tcp {
namespace Mixer {

Filter::Filter(Control& control) : control_(control) {
  ENVOY_LOG(debug, "Called tcp filter: {}", __func__);
}

Filter::~Filter() {
  cancelCheck();
  ENVOY_LOG(debug, "Called tcp filter : {}", __func__);
}

void Filter::initializeReadFilterCallbacks(
    Network::ReadFilterCallbacks& callbacks) {
  ENVOY_LOG(debug, "Called tcp filter: {}", __func__);
  filter_callbacks_ = &callbacks;
  filter_callbacks_->connection().addConnectionCallbacks(*this);
  start_time_ = std::chrono::system_clock::now();
}

void Filter::cancelCheck() {
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

// Makes a Check() call to Mixer.
void Filter::callCheck() {
  handler_ = control_.controller()->CreateRequestHandler();

  state_ = State::Calling;
  filter_callbacks_->connection().readDisable(true);
  calling_check_ = true;
  cancel_check_ = handler_->Check(this, [this](const CheckResponseInfo& info) {
    completeCheck(info.response_status);
  });
  calling_check_ = false;
}

// Network::ReadFilter
Network::FilterStatus Filter::onData(Buffer::Instance& data, bool) {
  if (state_ == State::NotStarted) {
    // By waiting to invoke the callCheck() at onData(), the call to Mixer
    // will have sufficient SSL information to fill the check Request.
    callCheck();
  }

  ENVOY_CONN_LOG(debug, "Called tcp filter onRead bytes: {}",
                 filter_callbacks_->connection(), data.length());
  received_bytes_ += data.length();

  return state_ == State::Calling ? Network::FilterStatus::StopIteration
                                  : Network::FilterStatus::Continue;
}

// Network::WriteFilter
Network::FilterStatus Filter::onWrite(Buffer::Instance& data, bool) {
  ENVOY_CONN_LOG(debug, "Called tcp filter onWrite bytes: {}",
                 filter_callbacks_->connection(), data.length());
  send_bytes_ += data.length();
  return Network::FilterStatus::Continue;
}

Network::FilterStatus Filter::onNewConnection() {
  ENVOY_CONN_LOG(debug,
                 "Called tcp filter onNewConnection: remote {}, local {}",
                 filter_callbacks_->connection(),
                 filter_callbacks_->connection().remoteAddress()->asString(),
                 filter_callbacks_->connection().localAddress()->asString());

  // Wait until onData() is invoked.
  return Network::FilterStatus::Continue;
}

void Filter::completeCheck(const Status& status) {
  ENVOY_LOG(debug, "Called tcp filter completeCheck: {}", status.ToString());
  cancel_check_ = nullptr;
  if (state_ == State::Closed) {
    return;
  }
  state_ = State::Completed;
  filter_callbacks_->connection().readDisable(false);

  if (!status.ok()) {
    filter_callbacks_->connection().close(
        Network::ConnectionCloseType::NoFlush);
  } else {
    if (!calling_check_) {
      filter_callbacks_->continueReading();
    }
    handler_->Report(this, ConnectionEvent::OPEN);
    report_timer_ =
        control_.dispatcher().createTimer([this]() { OnReportTimer(); });
    report_timer_->enableTimer(control_.config().report_interval_ms());
  }
}

// Network::ConnectionCallbacks
void Filter::onEvent(Network::ConnectionEvent event) {
  if (filter_callbacks_->upstreamHost()) {
    ENVOY_LOG(debug, "Called tcp filter onEvent: {} upstream {}",
              enumToInt(event),
              filter_callbacks_->upstreamHost()->address()->asString());
  } else {
    ENVOY_LOG(debug, "Called tcp filter onEvent: {}", enumToInt(event));
  }

  if (event == Network::ConnectionEvent::RemoteClose ||
      event == Network::ConnectionEvent::LocalClose) {
    if (state_ != State::Closed && handler_) {
      if (report_timer_) {
        report_timer_->disableTimer();
      }
      handler_->Report(this, ConnectionEvent::CLOSE);
    }
    cancelCheck();
  }
}

bool Filter::GetSourceIpPort(std::string* str_ip, int* port) const {
  return Utils::GetIpPort(filter_callbacks_->connection().remoteAddress()->ip(),
                          str_ip, port);
}
bool Filter::GetPrincipal(bool peer, std::string* user) const {
  return Utils::GetPrincipal(&filter_callbacks_->connection(), peer, user);
}

bool Filter::IsMutualTLS() const {
  return Utils::IsMutualTLS(&filter_callbacks_->connection());
}

bool Filter::GetRequestedServerName(std::string* name) const {
  return Utils::GetRequestedServerName(&filter_callbacks_->connection(), name);
}

bool Filter::GetDestinationIpPort(std::string* str_ip, int* port) const {
  if (filter_callbacks_->upstreamHost() &&
      filter_callbacks_->upstreamHost()->address()) {
    return Utils::GetIpPort(filter_callbacks_->upstreamHost()->address()->ip(),
                            str_ip, port);
  }
  return false;
}
bool Filter::GetDestinationUID(std::string* uid) const {
  if (filter_callbacks_->upstreamHost()) {
    return Utils::GetDestinationUID(
        *filter_callbacks_->upstreamHost()->metadata(), uid);
  }
  return false;
}
void Filter::GetReportInfo(
    ::istio::control::tcp::ReportData::ReportInfo* data) const {
  data->received_bytes = received_bytes_;
  data->send_bytes = send_bytes_;
  data->duration = std::chrono::duration_cast<std::chrono::nanoseconds>(
      std::chrono::system_clock::now() - start_time_);
}

std::string Filter::GetConnectionId() const {
  char connection_id_str[32];
  StringUtil::itoa(connection_id_str, 32, filter_callbacks_->connection().id());
  std::string uuid_connection_id = control_.uuid() + "-";
  uuid_connection_id.append(connection_id_str);
  return uuid_connection_id;
}

void Filter::OnReportTimer() {
  handler_->Report(this, ConnectionEvent::CONTINUE);
  report_timer_->enableTimer(control_.config().report_interval_ms());
}

}  // namespace Mixer
}  // namespace Tcp
}  // namespace Envoy
