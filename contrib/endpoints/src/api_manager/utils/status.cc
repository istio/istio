// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "contrib/endpoints/include/api_manager/utils/status.h"

#include <sstream>

#include "contrib/endpoints/src/api_manager/utils/marshalling.h"

using ::google::protobuf::util::error::Code;

namespace google {
namespace api_manager {
namespace utils {

Status::Status(int code, const std::string& message, ErrorCause error_cause)
    : code_(code == 200 ? Code::OK : code),
      message_(message),
      error_cause_(error_cause) {}

Status::Status(int code, const std::string& message)
    : Status(code, message, Status::INTERNAL) {}

Status::Status() : Status(Code::OK, "", Status::INTERNAL) {}

bool Status::operator==(const Status& x) const {
  if (code_ != x.code_ || message_ != x.message_ ||
      error_cause_ != x.error_cause_) {
    return false;
  }
  return true;
}

/* static */ std::string Status::CodeToString(int code) {
  // NGX error codes are negative. These are generally control codes.
  if (code < 0) {
    switch (code) {
      case -1:
        return "ERROR";
      case -2:
        return "AGAIN";
      case -3:
        return "BUSY";
      case -4:
        return "DONE";
      case -5:
        return "DECLINED";
      case -6:
        return "ABORT";
      default: {
        std::ostringstream ngx_out;
        ngx_out << "UNKNOWN(" << code << ")";
        return ngx_out.str();
      }
    }
  }

  // Codes from 0 to 99 are in the protobuf canonical space, so we map those to
  // their enum names.
  if (code < 100) {
    switch (code) {
      case Code::OK:
        return "OK";
      case Code::CANCELLED:
        return "CANCELLED";
      case Code::UNKNOWN:
        return "UNKNOWN";
      case Code::INVALID_ARGUMENT:
        return "INVALID_ARGUMENT";
      case Code::DEADLINE_EXCEEDED:
        return "DEADLINE_EXCEEDED";
      case Code::NOT_FOUND:
        return "NOT_FOUND";
      case Code::ALREADY_EXISTS:
        return "ALREADY_EXISTS";
      case Code::PERMISSION_DENIED:
        return "PERMISSION_DENIED";
      case Code::UNAUTHENTICATED:
        return "UNAUTHENTICATED";
      case Code::RESOURCE_EXHAUSTED:
        return "RESOURCE_EXHAUSTED";
      case Code::FAILED_PRECONDITION:
        return "FAILED_PRECONDITION";
      case Code::ABORTED:
        return "ABORTED";
      case Code::OUT_OF_RANGE:
        return "OUT_OF_RANGE";
      case Code::UNIMPLEMENTED:
        return "UNIMPLEMENTED";
      case Code::INTERNAL:
        return "INTERNAL";
      case Code::UNAVAILABLE:
        return "UNAVAILABLE";
      case Code::DATA_LOSS:
        return "DATA_LOSS";
      default: {
        std::ostringstream canonical_out;
        canonical_out << "UNKNOWN(" << code << ")";
        return canonical_out.str();
      }
    }
  }

  // Codes >= 100 are in the HTTP space, so we map to the HTTP meaning. We don't
  // cover all of the code extensions, so any leftovers result in a generic
  // error message with the code attached.
  switch (code) {
    case 100:
      return "CONTINUE";
    case 200:
      return "OK";
    case 201:
      return "CREATED";
    case 202:
      return "ACCEPTED";
    case 204:
      return "NO_CONTENT";
    case 301:
      return "MOVED_PERMANENTLY";
    case 302:
      return "FOUND";
    case 303:
      return "SEE_OTHER";
    case 304:
      return "NOT_MODIFIED";
    case 305:
      return "USE_PROXY";
    case 307:
      return "TEMPORARY_REDIRECT";
    case 308:
      return "PERMANENT_REDIRECT";
    case 400:
      return "BAD_REQUEST";
    case 401:
      return "UNAUTHORIZED";
    case 402:
      return "PAYMENT_REQUIRED";
    case 403:
      return "FORBIDDEN";
    case 404:
      return "NOT_FOUND";
    case 405:
      return "METHOD_NOT_ALLOWED";
    case 406:
      return "NOT_ACCEPTABLE";
    case 408:
      return "REQUEST_TIMEOUT";
    case 409:
      return "CONFLICT";
    case 410:
      return "GONE";
    case 411:
      return "LENGTH_REQUIRED";
    case 412:
      return "PRECONDITION_FAILED";
    case 413:
      return "PAYLOAD_TOO_LARGE";
    case 414:
      return "REQUEST_URI_TOO_LONG";
    case 415:
      return "UNSUPPORTED_MEDIA_TYPE";
    case 416:
      return "RANGE_NOT_SATISFIABLE";
    case 417:
      return "EXPECTATION_FAILED";
    case 418:
      return "IM_A_TEAPOT";
    case 419:
      return "AUTHENTICATION_TIMEOUT";
    case 428:
      return "PRECONDITION_REQUIRED";
    case 429:
      return "TOO_MANY_REQUESTS";
    case 431:
      return "REQUEST_HEADERS_TOO_LARGE";
    case 444:
      return "NO_RESPONSE";
    case 499:
      return "CLIENT_CLOSED_REQUEST";
    case 500:
      return "INTERNAL_SERVER_ERROR";
    case 501:
      return "NOT_IMPLEMENTED";
    case 502:
      return "BAD_GATEWAY";
    case 503:
      return "SERVICE_UNAVAILABLE";
    case 504:
      return "GATEWAY_TIMEOUT";
    default: {
      std::ostringstream http_out;
      if (code >= 200 && code < 300)
        http_out << "OK(";
      else if (code >= 400 && code < 500)
        http_out << "INVALID_REQUEST(";
      else if (code >= 500 && code < 600)
        http_out << "SERVER_ERROR(";
      else
        http_out << "UNKNOWN(";
      http_out << code << ")";
      return http_out.str();
    }
  }
}

/* static */ std::string Status::ErrorCauseToString(ErrorCause error_cause) {
  switch (error_cause) {
    default:
    case INTERNAL:
      return "internal";
    case APPLICATION:
      return "application";
    case AUTH:
      return "auth";
    case SERVICE_CONTROL:
      return "service_control";
  }
}

/* static */ Status Status::FromProto(
    const ::google::protobuf::util::Status& proto_status) {
  if (proto_status.ok()) {
    return OK;
  }
  return Status(proto_status.error_code(),
                proto_status.error_message().ToString());
}

::google::protobuf::util::Status Status::ToProto() const {
  ::google::protobuf::util::Status result(CanonicalCode(), message_);
  return result;
}

/* static */ const Status& Status::OK = Status();
/* static */ const Status& Status::DONE = Status(-4, "");

// Note: We return 400 instead of 412 for failed precondition as the meaning of
// 412 is more specific than FAILED_PRECONDITION. If a 412 is desired create an
// error using 412 and it will be mapped in the other direction to the
// FAILED_PRECONDITION canonical code as desired.
int Status::HttpCode() const {
  // If the code is already an HTTP error code, just return it intact.
  if (code_ >= 100) {
    return code_;
  }

  // Map NGINX status codes to HTTP status codes.
  if (code_ < 0) {
    switch (code_) {
      case -1:
        // ERROR -> INTERNAL ERROR
        return 500;
      case -2:
        // AGAIN -> CONTINUE
        return 100;
      case -3:
        // BUSY -> TOO MANY REQUESTS
        return 429;
      case -4:
        // DONE -> ACCEPTED
        return 202;
      case -5:
        // DECLINED -> NOT FOUND
        return 404;
      case -6:
        // ABORT -> BAD REQUEST
        return 400;
      default:
        // UNKNOWN -> INTERNAL_SERVER_ERROR
        return 500;
    }
  }

  // Map Canonical codes to HTTP status codes. This is based on the mapping
  // defined by the protobuf http error space.
  switch (code_) {
    case Code::OK:
      return 200;
    case Code::CANCELLED:
      return 499;
    case Code::UNKNOWN:
      return 500;
    case Code::INVALID_ARGUMENT:
      return 400;
    case Code::DEADLINE_EXCEEDED:
      return 504;
    case Code::NOT_FOUND:
      return 404;
    case Code::ALREADY_EXISTS:
      return 409;
    case Code::PERMISSION_DENIED:
      return 403;
    case Code::RESOURCE_EXHAUSTED:
      return 429;
    case Code::FAILED_PRECONDITION:
      return 400;
    case Code::ABORTED:
      return 409;
    case Code::OUT_OF_RANGE:
      return 400;
    case Code::UNIMPLEMENTED:
      return 501;
    case Code::INTERNAL:
      return 500;
    case Code::UNAVAILABLE:
      return 503;
    case Code::DATA_LOSS:
      return 500;
    case Code::UNAUTHENTICATED:
      return 401;
    default:
      return 500;
  }
}

Code Status::CanonicalCode() const {
  // Map NGNX status codes to canonical codes.
  if (code_ < 0) {
    switch (code_) {
      case -1:
        // ERROR -> INTERNAL
        return Code::INTERNAL;
      case -2:
        // AGAIN -> CANCELLED
        return Code::CANCELLED;
      case -3:
        // BUSY -> PERMISSION DENIED
        return Code::PERMISSION_DENIED;
      case -4:
        // DONE -> ALREADY EXISTS
        return Code::ALREADY_EXISTS;
      case -5:
        // DECLINED -> NOT FOUND
        return Code::NOT_FOUND;
      case -6:
        // ABORT -> ABORTED
        return Code::ABORTED;
      default:
        // UNKNOWN -> UNKNOWN;
        return Code::UNKNOWN;
    }
  }

  // The space from 0 to 99 is for canonical codes, so we leave it as is.
  if (code_ < 100) {
    return (Code)code_;
  }

  // Map HTTP error codes to canonical codes. This is based on the mapping
  // defined by the protobuf HTTP error space.
  switch (code_) {
    case 400:
      return Code::INVALID_ARGUMENT;
    case 403:
      return Code::PERMISSION_DENIED;
    case 404:
      return Code::NOT_FOUND;
    case 409:
      return Code::ABORTED;
    case 416:
      return Code::OUT_OF_RANGE;
    case 429:
      return Code::RESOURCE_EXHAUSTED;
    case 499:
      return Code::CANCELLED;
    case 504:
      return Code::DEADLINE_EXCEEDED;
    case 501:
      return Code::UNIMPLEMENTED;
    case 503:
      return Code::UNAVAILABLE;
    case 401:
      return Code::UNAUTHENTICATED;
    default: {
      if (code_ >= 200 && code_ < 300) return Code::OK;
      if (code_ >= 400 && code_ < 500) return Code::FAILED_PRECONDITION;
      if (code_ >= 500 && code_ < 600) return Code::INTERNAL;
      return Code::UNKNOWN;
    }
  }
}

::google::rpc::Status Status::ToCanonicalProto() const {
  ::google::rpc::Status status;
  status.set_code(CanonicalCode());
  status.set_message(message_);

  ::google::rpc::DebugInfo info;
  info.set_detail(Status::ErrorCauseToString(error_cause_));
  status.add_details()->PackFrom(info);

  return status;
}

std::string Status::ToJson() const {
  ::google::rpc::Status proto = ToCanonicalProto();
  std::string result;
  int options = JsonOptions::PRETTY_PRINT | JsonOptions::OUTPUT_DEFAULTS;
  Status status = ProtoToJson(proto, &result, options);
  if (!status.ok()) {
    // If translation failed, try outputting the json translation failure itself
    // as a JSON error. This should only happen if one of the error details had
    // an unresolvable type url.
    proto = status.ToCanonicalProto();
    status = ProtoToJson(proto, &result, options);
    if (!status.ok()) {
      // This should never happen but just in case we do a non-json response.
      result = "Unable to generate error response: ";
      result.append(status.message_);
    }
  }
  return result;
}

std::string Status::ToString() const {
  if (code_ == Code::OK) {
    return "OK";
  } else {
    if (message_.empty()) {
      return Status::CodeToString(code_);
    } else {
      return Status::CodeToString(code_) + ": " + message_;
    }
  }
}

}  // namespace utils
}  // namespace api_manager
}  // namespace google
