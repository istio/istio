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
#include "include/api_manager/utils/status.h"

#include "google/protobuf/any.pb.h"
#include "google/rpc/error_details.pb.h"
#include "google/rpc/status.pb.h"
#include "gtest/gtest.h"
#include "src/api_manager/utils/marshalling.h"

namespace google {
namespace api_manager {
namespace utils {

namespace {

using ::google::protobuf::util::error::Code;

TEST(Status, StatusOkReturnsTrueWhenAppropriate) {
  EXPECT_TRUE(Status::OK.ok());
  EXPECT_TRUE(Status(0, "OK").ok());
  EXPECT_TRUE(Status(200, "OK").ok());

  EXPECT_FALSE(Status(-1, "Internal Error").ok());
  EXPECT_FALSE(Status(400, "Invalid Parameter").ok());
  EXPECT_FALSE(Status(5, "Not Found").ok());
}

TEST(Status, ToHttpCodeMapping) {
  EXPECT_EQ(200, Status::OK.HttpCode());
  EXPECT_EQ(404, Status(5, "Not Found").HttpCode());
  EXPECT_EQ(200, Status(200, "OK").HttpCode());

  // Nginx codes
  EXPECT_EQ(500, Status(-1, "").HttpCode());
  EXPECT_EQ(100, Status(-2, "").HttpCode());
  EXPECT_EQ(429, Status(-3, "").HttpCode());
  EXPECT_EQ(202, Status(-4, "").HttpCode());
  EXPECT_EQ(404, Status(-5, "").HttpCode());
  EXPECT_EQ(400, Status(-6, "").HttpCode());
  EXPECT_EQ(500, Status(-7, "").HttpCode());

  // Canonical codes.
  EXPECT_EQ(200, Status(Code::OK, "").HttpCode());
  EXPECT_EQ(499, Status(Code::CANCELLED, "").HttpCode());
  EXPECT_EQ(500, Status(Code::UNKNOWN, "").HttpCode());
  EXPECT_EQ(400, Status(Code::INVALID_ARGUMENT, "").HttpCode());
  EXPECT_EQ(504, Status(Code::DEADLINE_EXCEEDED, "").HttpCode());
  EXPECT_EQ(404, Status(Code::NOT_FOUND, "").HttpCode());
  EXPECT_EQ(409, Status(Code::ALREADY_EXISTS, "").HttpCode());
  EXPECT_EQ(403, Status(Code::PERMISSION_DENIED, "").HttpCode());
  EXPECT_EQ(401, Status(Code::UNAUTHENTICATED, "").HttpCode());
  EXPECT_EQ(429, Status(Code::RESOURCE_EXHAUSTED, "").HttpCode());
  EXPECT_EQ(400, Status(Code::FAILED_PRECONDITION, "").HttpCode());
  EXPECT_EQ(409, Status(Code::ABORTED, "").HttpCode());
  EXPECT_EQ(400, Status(Code::OUT_OF_RANGE, "").HttpCode());
  EXPECT_EQ(501, Status(Code::UNIMPLEMENTED, "").HttpCode());
  EXPECT_EQ(500, Status(Code::INTERNAL, "").HttpCode());
  EXPECT_EQ(503, Status(Code::UNAVAILABLE, "").HttpCode());
  EXPECT_EQ(500, Status(Code::DATA_LOSS, "").HttpCode());
  EXPECT_EQ(500, Status(70, "").HttpCode());
}

TEST(Status, ToCanonicalMapping) {
  EXPECT_EQ(0, Status::OK.CanonicalCode());
  EXPECT_EQ(5, Status(404, "Not Found").CanonicalCode());
  EXPECT_EQ(4, Status(4, "Deadline Exceeded").CanonicalCode());

  EXPECT_EQ(Code::OK, Status(200, "").CanonicalCode());
  EXPECT_EQ(Code::CANCELLED, Status(-2, "").CanonicalCode());
  EXPECT_EQ(Code::UNKNOWN, Status(700, "").CanonicalCode());
  EXPECT_EQ(Code::INVALID_ARGUMENT, Status(400, "").CanonicalCode());
  EXPECT_EQ(Code::DEADLINE_EXCEEDED, Status(504, "").CanonicalCode());
  EXPECT_EQ(Code::NOT_FOUND, Status(-5, "").CanonicalCode());
  EXPECT_EQ(Code::NOT_FOUND, Status(404, "").CanonicalCode());
  EXPECT_EQ(Code::ALREADY_EXISTS, Status(-4, "").CanonicalCode());
  EXPECT_EQ(Code::PERMISSION_DENIED, Status(-3, "").CanonicalCode());
  EXPECT_EQ(Code::PERMISSION_DENIED, Status(403, "").CanonicalCode());
  EXPECT_EQ(Code::UNAUTHENTICATED, Status(401, "").CanonicalCode());
  EXPECT_EQ(Code::RESOURCE_EXHAUSTED, Status(429, "").CanonicalCode());
  EXPECT_EQ(Code::FAILED_PRECONDITION, Status(450, "").CanonicalCode());
  EXPECT_EQ(Code::ABORTED, Status(409, "").CanonicalCode());
  EXPECT_EQ(Code::OUT_OF_RANGE, Status(416, "").CanonicalCode());
  EXPECT_EQ(Code::UNIMPLEMENTED, Status(501, "").CanonicalCode());
  EXPECT_EQ(Code::INTERNAL, Status(500, "").CanonicalCode());
  EXPECT_EQ(Code::UNAVAILABLE, Status(503, "").CanonicalCode());
}

TEST(Status, ToProtoIncludesCodeAndMessage) {
  Status status(400, "Invalid Parameter");
  ::google::protobuf::util::Status proto = status.ToProto();
  EXPECT_EQ(Code::INVALID_ARGUMENT, proto.error_code());
  EXPECT_EQ("Invalid Parameter", proto.error_message());
}

TEST(Status, ToJsonIncludesCodeAndMessage) {
  EXPECT_EQ(
      "{\n"
      " \"code\": 5,\n"
      " \"message\": \"Unknown Element\",\n"
      " \"details\": [\n"
      "  {\n"
      "   \"@type\": \"type.googleapis.com/google.rpc.DebugInfo\",\n"
      "   \"stackEntries\": [],\n"
      "   \"detail\": \"auth\"\n"
      "  }\n"
      " ]\n"
      "}\n",
      Status(5, "Unknown Element", Status::AUTH).ToJson());
}

TEST(Status, ToJsonIncludesDetails) {
  Status status(400, "Invalid Parameter");

  EXPECT_EQ(
      "{\n"
      " \"code\": 3,\n"
      " \"message\": \"Invalid Parameter\",\n"
      " \"details\": [\n"
      "  {\n"
      "   \"@type\": \"type.googleapis.com/google.rpc.DebugInfo\",\n"
      "   \"stackEntries\": [],\n"
      "   \"detail\": \"internal\"\n"
      "  }\n"
      " ]\n"
      "}\n",
      status.ToJson());
}

TEST(Status, ToStringPrintsNgxCodes) {
  EXPECT_EQ("ERROR", Status(-1, "").ToString());
  EXPECT_EQ("AGAIN", Status(-2, "").ToString());
  EXPECT_EQ("BUSY", Status(-3, "").ToString());
  EXPECT_EQ("DONE", Status(-4, "").ToString());
  EXPECT_EQ("DECLINED", Status(-5, "").ToString());
  EXPECT_EQ("ABORT", Status(-6, "").ToString());
  EXPECT_EQ("UNKNOWN(-17)", Status(-17, "").ToString());
  EXPECT_EQ("AGAIN: Try Again!", Status(-2, "Try Again!").ToString());
  EXPECT_EQ("ABORT: Uh oh", Status(-6, "Uh oh").ToString());
}

TEST(Status, ToStringPrintsCanonicalCodes) {
  EXPECT_EQ("OK", Status(Code::OK, "").ToString());
  EXPECT_EQ("CANCELLED", Status(Code::CANCELLED, "").ToString());
  EXPECT_EQ("UNKNOWN", Status(Code::UNKNOWN, "").ToString());
  EXPECT_EQ("INVALID_ARGUMENT", Status(Code::INVALID_ARGUMENT, "").ToString());
  EXPECT_EQ("DEADLINE_EXCEEDED",
            Status(Code::DEADLINE_EXCEEDED, "").ToString());
  EXPECT_EQ("NOT_FOUND", Status(Code::NOT_FOUND, "").ToString());
  EXPECT_EQ("ALREADY_EXISTS", Status(Code::ALREADY_EXISTS, "").ToString());
  EXPECT_EQ("PERMISSION_DENIED",
            Status(Code::PERMISSION_DENIED, "").ToString());
  EXPECT_EQ("UNAUTHENTICATED", Status(Code::UNAUTHENTICATED, "").ToString());
  EXPECT_EQ("RESOURCE_EXHAUSTED",
            Status(Code::RESOURCE_EXHAUSTED, "").ToString());
  EXPECT_EQ("FAILED_PRECONDITION",
            Status(Code::FAILED_PRECONDITION, "").ToString());
  EXPECT_EQ("ABORTED", Status(Code::ABORTED, "").ToString());
  EXPECT_EQ("OUT_OF_RANGE", Status(Code::OUT_OF_RANGE, "").ToString());
  EXPECT_EQ("UNIMPLEMENTED", Status(Code::UNIMPLEMENTED, "").ToString());
  EXPECT_EQ("INTERNAL", Status(Code::INTERNAL, "").ToString());
  EXPECT_EQ("UNAVAILABLE", Status(Code::UNAVAILABLE, "").ToString());
  EXPECT_EQ("DATA_LOSS", Status(Code::DATA_LOSS, "").ToString());

  EXPECT_EQ("UNKNOWN(50): Unknown error code.",
            Status(50, "Unknown error code.").ToString());

  EXPECT_EQ("INVALID_ARGUMENT: Unknown parameter 'foo'",
            Status(3, "Unknown parameter 'foo'").ToString());
  EXPECT_EQ("UNIMPLEMENTED: Support forthcoming",
            Status(12, "Support forthcoming").ToString());
}

TEST(Status, ToStringPrintsHttpCodes) {
  EXPECT_EQ("INTERNAL_SERVER_ERROR: Bad configuration",
            Status(500, "Bad configuration").ToString());
  EXPECT_EQ("IM_A_TEAPOT: Short and stout",
            Status(418, "Short and stout").ToString());

  EXPECT_EQ("CONTINUE", Status(100, "").ToString());
  EXPECT_EQ("OK", Status(200, "").ToString());
  EXPECT_EQ("CREATED", Status(201, "").ToString());
  EXPECT_EQ("ACCEPTED", Status(202, "").ToString());
  EXPECT_EQ("NO_CONTENT", Status(204, "").ToString());
  EXPECT_EQ("MOVED_PERMANENTLY", Status(301, "").ToString());
  EXPECT_EQ("FOUND", Status(302, "").ToString());
  EXPECT_EQ("SEE_OTHER", Status(303, "").ToString());
  EXPECT_EQ("NOT_MODIFIED", Status(304, "").ToString());
  EXPECT_EQ("USE_PROXY", Status(305, "").ToString());
  EXPECT_EQ("TEMPORARY_REDIRECT", Status(307, "").ToString());
  EXPECT_EQ("PERMANENT_REDIRECT", Status(308, "").ToString());
  EXPECT_EQ("BAD_REQUEST", Status(400, "").ToString());
  EXPECT_EQ("UNAUTHORIZED", Status(401, "").ToString());
  EXPECT_EQ("PAYMENT_REQUIRED", Status(402, "").ToString());
  EXPECT_EQ("FORBIDDEN", Status(403, "").ToString());
  EXPECT_EQ("NOT_FOUND", Status(404, "").ToString());
  EXPECT_EQ("METHOD_NOT_ALLOWED", Status(405, "").ToString());
  EXPECT_EQ("NOT_ACCEPTABLE", Status(406, "").ToString());
  EXPECT_EQ("REQUEST_TIMEOUT", Status(408, "").ToString());
  EXPECT_EQ("CONFLICT", Status(409, "").ToString());
  EXPECT_EQ("GONE", Status(410, "").ToString());
  EXPECT_EQ("LENGTH_REQUIRED", Status(411, "").ToString());
  EXPECT_EQ("PRECONDITION_FAILED", Status(412, "").ToString());
  EXPECT_EQ("PAYLOAD_TOO_LARGE", Status(413, "").ToString());
  EXPECT_EQ("REQUEST_URI_TOO_LONG", Status(414, "").ToString());
  EXPECT_EQ("UNSUPPORTED_MEDIA_TYPE", Status(415, "").ToString());
  EXPECT_EQ("RANGE_NOT_SATISFIABLE", Status(416, "").ToString());
  EXPECT_EQ("EXPECTATION_FAILED", Status(417, "").ToString());
  EXPECT_EQ("IM_A_TEAPOT", Status(418, "").ToString());
  EXPECT_EQ("AUTHENTICATION_TIMEOUT", Status(419, "").ToString());
  EXPECT_EQ("PRECONDITION_REQUIRED", Status(428, "").ToString());
  EXPECT_EQ("TOO_MANY_REQUESTS", Status(429, "").ToString());
  EXPECT_EQ("REQUEST_HEADERS_TOO_LARGE", Status(431, "").ToString());
  EXPECT_EQ("NO_RESPONSE", Status(444, "").ToString());
  EXPECT_EQ("CLIENT_CLOSED_REQUEST", Status(499, "").ToString());
  EXPECT_EQ("INTERNAL_SERVER_ERROR", Status(500, "").ToString());
  EXPECT_EQ("NOT_IMPLEMENTED", Status(501, "").ToString());
  EXPECT_EQ("BAD_GATEWAY", Status(502, "").ToString());
  EXPECT_EQ("SERVICE_UNAVAILABLE", Status(503, "").ToString());
  EXPECT_EQ("GATEWAY_TIMEOUT", Status(504, "").ToString());
}

TEST(Status, ToStringPrintsIntegerCodeForUnknownHttpCodes) {
  EXPECT_EQ("SERVER_ERROR(529): Something Screwy",
            Status(529, "Something Screwy").ToString());
  EXPECT_EQ("INVALID_REQUEST(472): I don't even",
            Status(472, "I don't even").ToString());
  EXPECT_EQ("OK(237): OK I guess", Status(237, "OK I guess").ToString());
  EXPECT_EQ("UNKNOWN(987): Nine, Eight, Seven",
            Status(987, "Nine, Eight, Seven").ToString());
}

}  // namespace

}  // namespace utils
}  // namespace api_manager
}  // namespace google
