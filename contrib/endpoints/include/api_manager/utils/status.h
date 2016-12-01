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
#ifndef API_MANAGER_UTILS_STATUS_H_
#define API_MANAGER_UTILS_STATUS_H_

#include <string>
#include <vector>

#include "google/protobuf/any.pb.h"
#include "google/protobuf/stubs/status.h"
#include "google/rpc/error_details.pb.h"
#include "google/rpc/status.pb.h"

using ::google::protobuf::util::error::Code;

namespace google {
namespace api_manager {
namespace utils {

// A Status object can be used to represent an error or an OK state. Error
// status messages have an error code, an error message, and an error cause.
// An OK status has a code of 0 or 200 and no message.
class Status final {
 public:
  enum ErrorCause {
    // Internal proxy error (default)
    INTERNAL = 0,
    // External application error
    APPLICATION = 1,
    // Error in authentication
    AUTH = 2,
    // Error in service control check
    SERVICE_CONTROL = 3
  };

  // Constructs a status with an error code and message. If code == 0
  // message is ignored and a Status object identical to Status::OK
  // is constructed. Error cause is optional and defaults to INTERNAL.
  Status(int code, const std::string& message);
  Status(int code, const std::string& message, ErrorCause error_cause);
  ~Status() {}

  bool operator==(const Status& x) const;
  bool operator!=(const Status& x) const { return !operator==(x); }

  // Get string representation of the error code
  static std::string CodeToString(int code);

  // Get string representation of the error cause
  static std::string ErrorCauseToString(ErrorCause error_cause);

  // Constructs a Status object from a protobuf Status.
  static Status FromProto(const ::google::protobuf::util::Status& proto_status);

  // Pre-defined OK status.
  static const Status& OK;

  // Pre-defined DONE status.
  static const Status& DONE;

  // Returns true if this status is not an error
  bool ok() const { return code_ == Code::OK || code_ == 200; }

  // Returns the error code held by this status.
  int code() const { return code_; }

  // Returns the error message held by this status.
  const std::string& message() const { return message_; }

  // Returns the error cause held by this status.
  ErrorCause error_cause() const { return error_cause_; }

  // Returns the error code mapped to HTTP status codes.
  int HttpCode() const;

  // Returns the error code mapped to protobuf canonical code.
  Code CanonicalCode() const;

  // Returns a combination of the error code name and message.
  std::string ToString() const;

  // Returns a representation of the error as a protobuf Status.
  ::google::protobuf::util::Status ToProto() const;

  // Returns a representation of the error as a canonical status
  ::google::rpc::Status ToCanonicalProto() const;

  // Returns a JSON representation of the error as a canonical status
  std::string ToJson() const;

 private:
  // Constructs the OK status.
  Status();

  // Error code. Zero means OK. Negative numbers are for control
  // statuses (e.g. DECLINED). Positive numbers below 100 represent grpc
  // status codes. Positive numbers 100 and greater represent HTTP status codes.
  int code_;

  // The error message if this Status represents an error, otherwise an empty
  // string if this is the OK status.
  std::string message_;

  // Error cause indicating the origin of the error.
  ErrorCause error_cause_;
};

}  // namespace utils
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_UTILS_STATUS_H_
