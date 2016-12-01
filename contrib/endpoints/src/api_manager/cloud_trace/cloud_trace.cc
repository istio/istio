// Copyright (C) Extensible Service Proxy Authors
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "cloud_trace.h"

#include <cctype>
#include <chrono>
#include <iomanip>
#include <random>
#include <sstream>
#include <string>
#include "google/protobuf/timestamp.pb.h"
#include "include/api_manager/utils/status.h"
#include "include/api_manager/version.h"
#include "src/api_manager/utils/marshalling.h"

using google::api_manager::utils::Status;
using google::devtools::cloudtrace::v1::Trace;
using google::devtools::cloudtrace::v1::Traces;
using google::devtools::cloudtrace::v1::TraceSpan;
using google::devtools::cloudtrace::v1::TraceSpan_SpanKind;
using google::protobuf::Timestamp;

namespace google {
namespace api_manager {
namespace cloud_trace {
namespace {

const char kCloudTraceService[] = "/google.devtools.cloudtrace.v1.TraceService";
// Cloud Trace agent label key
const char kCloudTraceAgentKey[] = "trace.cloud.google.com/agent";
// Cloud Trace agent label value
const char kServiceAgent[] = "esp/" API_MANAGER_VERSION_STRING;
// Default trace options
const char kDefaultTraceOptions[] = "o=1";

// Generate a random unsigned 64-bit integer.
uint64_t RandomUInt64();

// Get a random string of 128 bit hex number
std::string RandomUInt128HexString();

// Get the timestamp for now.
void GetNow(Timestamp *ts);

// Get a new Trace object stored in the trace parameter. The new object has
// the given trace id and contains a root span with default settings.
void GetNewTrace(std::string trace_id_str, const std::string &root_span_name,
                 Trace **trace);

// Parse the trace context header.
// Assigns Trace object to the trace pointer if context is parsed correctly and
// trace is enabled. Otherwise the pointer is not modified.
// If trace is enabled, the option will be modified to the one passed in.
//
// Grammar of the context header:
// trace-id  [“/” span-id] [ “;” “o” “=” trace_options ]
//
// trace-id      := hex representation of a 128 bit value
// span-id       := decimal representation of a 64 bit value
// trace-options := decimal representation of a 32 bit value
//
void GetTraceFromContextHeader(const std::string &trace_context,
                               const std::string &root_span_name, Trace **trace,
                               std::string *options);
}  // namespace

Sampler::Sampler(double qps) {
  if (qps == 0.0) {
    is_disabled_ = true;
  } else {
    duration_ = 1.0 / qps;
    is_disabled_ = false;
  }
}

bool Sampler::On() {
  if (is_disabled_) {
    return false;
  }
  auto now = std::chrono::system_clock::now();
  std::chrono::duration<double> diff = now - previous_;
  if (diff.count() > duration_) {
    previous_ = now;
    return true;
  } else {
    return false;
  }
};

void Sampler::Refresh() {
  if (is_disabled_) {
    return;
  }
  previous_ = std::chrono::system_clock::now();
}

Aggregator::Aggregator(auth::ServiceAccountToken *sa_token,
                       const std::string &cloud_trace_address,
                       int aggregate_time_millisec, int cache_max_size,
                       double minimum_qps, ApiManagerEnvInterface *env)
    : sa_token_(sa_token),
      cloud_trace_address_(cloud_trace_address),
      aggregate_time_millisec_(aggregate_time_millisec),
      cache_max_size_(cache_max_size),
      traces_(new Traces),
      env_(env),
      sampler_(minimum_qps) {
  sa_token_->SetAudience(auth::ServiceAccountToken::JWT_TOKEN_FOR_CLOUD_TRACING,
                         cloud_trace_address_ + kCloudTraceService);
}

void Aggregator::Init() {
  if (aggregate_time_millisec_ == 0) {
    return;
  }
  timer_ = env_->StartPeriodicTimer(
      std::chrono::milliseconds(aggregate_time_millisec_),
      [this]() { SendAndClearTraces(); });
}

Aggregator::~Aggregator() {
  if (timer_) {
    timer_->Stop();
  }
}

void Aggregator::SendAndClearTraces() {
  if (traces_->traces_size() == 0 || project_id_.empty()) {
    env_->LogDebug(
        "Not sending request to CloudTrace: no traces or "
        "project_id is empty.");
    traces_->clear_traces();
    return;
  }

  // Add project id into each trace object.
  for (int i = 0; i < traces_->traces_size(); ++i) {
    traces_->mutable_traces(i)->set_project_id(project_id_);
  }

  std::unique_ptr<HTTPRequest> http_request(new HTTPRequest(
      [this](Status status, std::map<std::string, std::string> &&,
             std::string &&body) {
        if (status.code() < 0) {
          env_->LogError("Trace Request Failed." + status.ToString());
        } else {
          env_->LogDebug("Trace Response: " + status.ToString() + "\n" + body);
        }
      }));

  std::string url =
      cloud_trace_address_ + "/v1/projects/" + project_id_ + "/traces";

  std::string request_body;

  ProtoToJson(*traces_, &request_body, utils::DEFAULT);
  traces_->clear_traces();
  env_->LogDebug("Sending request to Cloud Trace.");
  env_->LogDebug(request_body);

  http_request->set_url(url)
      .set_method("PATCH")
      .set_auth_token(sa_token_->GetAuthToken(
          auth::ServiceAccountToken::JWT_TOKEN_FOR_CLOUD_TRACING))
      .set_header("Content-Type", "application/json")
      .set_body(request_body);

  env_->RunHTTPRequest(std::move(http_request));
}

void Aggregator::AppendTrace(google::devtools::cloudtrace::v1::Trace *trace) {
  traces_->mutable_traces()->AddAllocated(trace);
  if (traces_->traces_size() > cache_max_size_) {
    SendAndClearTraces();
  }
}

CloudTrace::CloudTrace(Trace *trace, const std::string &options)
    : trace_(trace), options_(options) {
  // Root span must exist and must be the only span as of now.
  root_span_ = trace_->mutable_spans(0);
}

void CloudTrace::SetProjectId(const std::string &project_id) {
  trace_->set_project_id(project_id);
}

void CloudTrace::EndRootSpan() { GetNow(root_span_->mutable_end_time()); }

CloudTraceSpan::CloudTraceSpan(CloudTrace *cloud_trace,
                               const std::string &span_name)
    : cloud_trace_(cloud_trace) {
  InitWithParentSpanId(span_name, cloud_trace_->root_span()->span_id());
}

CloudTraceSpan::CloudTraceSpan(CloudTraceSpan *parent,
                               const std::string &span_name)
    : cloud_trace_(parent->cloud_trace_) {
  InitWithParentSpanId(span_name, parent->trace_span_->span_id());
}

void CloudTraceSpan::InitWithParentSpanId(const std::string &span_name,
                                          protobuf::uint64 parent_span_id) {
  // TODO: this if is not needed, and probably the following two as well.
  // Fully test and remove them.
  if (!cloud_trace_) {
    // Trace is disabled.
    return;
  }
  trace_span_ = cloud_trace_->trace()->add_spans();
  trace_span_->set_kind(TraceSpan_SpanKind::TraceSpan_SpanKind_RPC_SERVER);
  trace_span_->set_span_id(RandomUInt64());
  trace_span_->set_parent_span_id(parent_span_id);
  trace_span_->set_name(span_name);
  GetNow(trace_span_->mutable_start_time());
}

CloudTraceSpan::~CloudTraceSpan() {
  if (!cloud_trace_) {
    // Trace is disabled.
    return;
  }
  GetNow(trace_span_->mutable_end_time());
  for (unsigned int i = 0; i < messages.size(); ++i) {
    std::stringstream stream;
    stream << std::setfill('0') << std::setw(3) << i;
    std::string sequence = stream.str();
    trace_span_->mutable_labels()->insert({sequence, messages[i]});
  }
}

void CloudTraceSpan::Write(const std::string &msg) {
  if (!cloud_trace_) {
    // Trace is disabled.
    return;
  }
  messages.push_back(msg);
}

CloudTrace *CreateCloudTrace(const std::string &trace_context,
                             const std::string &root_span_name,
                             Sampler *sampler) {
  Trace *trace = nullptr;
  std::string options;
  GetTraceFromContextHeader(trace_context, root_span_name, &trace, &options);
  if (trace) {
    // When trace is triggered by the context header, refresh the previous
    // timestamp in sampler.
    if (sampler) {
      sampler->Refresh();
    }
    return new CloudTrace(trace, options);
  } else if (sampler && sampler->On()) {
    // Trace is turned on by sampler.
    GetNewTrace(RandomUInt128HexString(), root_span_name, &trace);
    return new CloudTrace(trace, kDefaultTraceOptions);
  } else {
    return nullptr;
  }
}

CloudTraceSpan *CreateSpan(CloudTrace *cloud_trace, const std::string &name) {
  if (cloud_trace != nullptr) {
    return new CloudTraceSpan(cloud_trace, name);
  } else {
    return nullptr;
  }
}

CloudTraceSpan *CreateChildSpan(CloudTraceSpan *parent,
                                const std::string &name) {
  if (parent != nullptr) {
    return new CloudTraceSpan(parent, name);
  } else {
    return nullptr;
  }
}

TraceStream::~TraceStream() { trace_span_->Write(info_.str()); }

namespace {
// TODO: this method is duplicated with a similar method in
// api_manager/context/request_context.cc. Consider merge them by moving to a
// common position.
uint64_t RandomUInt64() {
  static std::random_device random_device;
  static std::mt19937 generator(random_device());
  static std::uniform_int_distribution<uint64_t> distribution;
  return distribution(generator);
}

std::string RandomUInt128HexString() {
  std::stringstream stream;

  stream << std::setfill('0') << std::setw(sizeof(uint64_t) * 2) << std::hex
         << RandomUInt64();
  stream << std::setfill('0') << std::setw(sizeof(uint64_t) * 2) << std::hex
         << RandomUInt64();

  return stream.str();
}

void GetNow(Timestamp *ts) {
  long long nanos =
      std::chrono::duration_cast<std::chrono::nanoseconds>(
          std::chrono::high_resolution_clock::now().time_since_epoch())
          .count();
  ts->set_seconds(nanos / 1000000000);
  ts->set_nanos(nanos % 1000000000);
}

void GetNewTrace(std::string trace_id_str, const std::string &root_span_name,
                 Trace **trace) {
  *trace = new Trace;
  (*trace)->set_trace_id(trace_id_str);
  TraceSpan *root_span = (*trace)->add_spans();
  root_span->set_kind(TraceSpan_SpanKind::TraceSpan_SpanKind_RPC_SERVER);
  root_span->set_span_id(RandomUInt64());
  root_span->set_name(root_span_name);
  // Agent label is defined as "<agent>/<version>".
  root_span->mutable_labels()->insert({kCloudTraceAgentKey, kServiceAgent});
  GetNow(root_span->mutable_start_time());
}

void GetTraceFromContextHeader(const std::string &trace_context,
                               const std::string &root_span_name, Trace **trace,
                               std::string *options) {
  std::stringstream header_stream(trace_context);

  std::string trace_and_span_id;
  if (!getline(header_stream, trace_and_span_id, ';')) {
    // When trace_context is empty;
    return;
  }

  bool trace_enabled = false;
  std::string item;
  while (getline(header_stream, item, ';')) {
    if (item.substr(0, 2) == "o=") {
      int value;
      std::stringstream option_stream(item.substr(2));
      if ((option_stream >> value).fail() || !option_stream.eof()) {
        return;
      }
      if (value < 0 || value > 0b11) {
        // invalid option value.
        return;
      }
      *options = trace_context.substr(trace_context.find_first_of(';') + 1);
      // First bit indicates whether trace is enabled.
      if (!(value & 1)) {
        return;
      }
      // Trace is enabled, we can stop parsing the header.
      trace_enabled = true;
      break;
    }
  }
  if (!trace_enabled) {
    return;
  }

  // Parse trace_id/span_id
  std::string trace_id_str, span_id_str;
  size_t slash_pos = trace_and_span_id.find_first_of('/');
  if (slash_pos == std::string::npos) {
    trace_id_str = trace_and_span_id;
  } else {
    trace_id_str = trace_and_span_id.substr(0, slash_pos);
    span_id_str = trace_and_span_id.substr(slash_pos + 1);
  }

  // Trace id should be a 128-bit hex number (32 hex digits).
  if (trace_id_str.size() != 32) {
    return;
  }
  for (size_t i = 0; i < trace_id_str.size(); ++i) {
    if (!isxdigit(trace_id_str[i])) {
      return;
    }
  }

  uint64_t span_id = 0;
  // Parse the span id, disable trace when span id is illegal.
  if (!span_id_str.empty()) {
    std::stringstream span_id_stream(span_id_str);
    if ((span_id_stream >> span_id).fail() || !span_id_stream.eof()) {
      return;
    }
  }

  // At this point, trace is enabled and trace id is successfully parsed.
  GetNewTrace(trace_id_str, root_span_name, trace);
  TraceSpan *root_span = (*trace)->mutable_spans(0);
  // Set parent of root span to the given one if provided.
  if (span_id != 0) {
    root_span->set_parent_span_id(span_id);
  }
}

}  // namespace
}  // cloud_trace
}  // api_manager
}  // google
