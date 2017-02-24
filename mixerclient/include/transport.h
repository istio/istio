/* Copyright 2017 Google Inc. All Rights Reserved.
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

#ifndef MIXERCLIENT_TRANSPORT_H
#define MIXERCLIENT_TRANSPORT_H

#include <functional>
#include <memory>
#include <string>

#include "google/protobuf/stubs/status.h"
#include "mixer/v1/service.pb.h"

namespace istio {
namespace mixer_client {

// A stream write interface implemented by transport layer
// It will be called by Mixer client.
template <class RequestType>
class WriteInterface {
 public:
  virtual ~WriteInterface() {}
  // Write a request message.
  virtual void Write(const RequestType &) = 0;
  // Half close the write direction.
  virtual void WritesDone() = 0;
  // If true, write direction is closed.
  virtual bool is_write_closed() const = 0;
};

// A stream read interface implemented by Mixer client
// to receive response data, or stream close status.
// It will be called by the transport layer.
template <class ResponseType>
class ReadInterface {
 public:
  virtual ~ReadInterface() {}
  // On receive a response message
  virtual void OnRead(const ResponseType &) = 0;
  // On stream close.
  virtual void OnClose(const ::google::protobuf::util::Status &) = 0;
};

typedef std::shared_ptr<WriteInterface<::istio::mixer::v1::CheckRequest>>
    CheckWriterPtr;
typedef std::shared_ptr<WriteInterface<::istio::mixer::v1::ReportRequest>>
    ReportWriterPtr;
typedef std::shared_ptr<WriteInterface<::istio::mixer::v1::QuotaRequest>>
    QuotaWriterPtr;

typedef ReadInterface<::istio::mixer::v1::CheckResponse> *CheckReaderRawPtr;
typedef ReadInterface<::istio::mixer::v1::ReportResponse> *ReportReaderRawPtr;
typedef ReadInterface<::istio::mixer::v1::QuotaResponse> *QuotaReaderRawPtr;

// This is the transport interface needed by Mixer client.
// The callers of the Mixer client need to implement this interface and
// pass it to the client.
class TransportInterface {
 public:
  virtual ~TransportInterface() {}
  // Create a Check stream
  virtual CheckWriterPtr NewStream(CheckReaderRawPtr) = 0;
  // Create a Report stream
  virtual ReportWriterPtr NewStream(ReportReaderRawPtr) = 0;
  // Create a Quota stream
  virtual QuotaWriterPtr NewStream(QuotaReaderRawPtr) = 0;
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXERCLIENT_TRANSPORT_H
