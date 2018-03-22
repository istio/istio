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

#include "src/envoy/http/authn/http_filter.h"
#include "authentication/v1alpha1/policy.pb.h"
#include "common/common/base64.h"
#include "common/http/header_map_impl.h"
#include "gmock/gmock.h"
#include "gtest/gtest.h"
#include "src/envoy/http/authn/authenticator_base.h"
#include "src/envoy/http/authn/test_utils.h"
#include "test/mocks/http/mocks.h"
#include "test/test_common/utility.h"

using Envoy::Http::Istio::AuthN::AuthenticatorBase;
using Envoy::Http::Istio::AuthN::FilterContext;
using istio::authn::Result;
using testing::Invoke;
using testing::NiceMock;
using testing::StrictMock;
using testing::_;

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {
namespace {

// Create a fake authenticator for test. This authenticator do nothing except
// making done callback with failure.
std::unique_ptr<AuthenticatorBase> createAlwaysFailAuthenticator(
    FilterContext* filter_context,
    const AuthenticatorBase::DoneCallback& done_callback) {
  class _local : public AuthenticatorBase {
   public:
    _local(FilterContext* filter_context,
           const AuthenticatorBase::DoneCallback& callback)
        : AuthenticatorBase(filter_context, callback) {}
    void run() override { done(false); }
  };
  return std::make_unique<_local>(filter_context, done_callback);
}

// Create a fake authenticator for test. This authenticator do nothing except
// making done callback with success.
std::unique_ptr<AuthenticatorBase> createAlwaysPassAuthenticator(
    FilterContext* filter_context,
    const AuthenticatorBase::DoneCallback& done_callback) {
  class _local : public AuthenticatorBase {
   public:
    _local(FilterContext* filter_context,
           const AuthenticatorBase::DoneCallback& callback)
        : AuthenticatorBase(filter_context, callback) {}
    void run() override {
      // Set some data to verify authentication result later.
      auto payload = TestUtilities::CreateX509Payload("foo");
      filter_context()->setPeerResult(&payload);
      done(true);
    }
  };
  return std::make_unique<_local>(filter_context, done_callback);
}

class MockAuthenticationFilter : public AuthenticationFilter {
 public:
  // We'll use fake authenticator for test, so policy is not really needed. Use
  // default policy for simplicity.
  MockAuthenticationFilter()
      : AuthenticationFilter(
            istio::authentication::v1alpha1::Policy::default_instance()) {}
  ~MockAuthenticationFilter(){};

  MOCK_METHOD2(createPeerAuthenticator,
               std::unique_ptr<AuthenticatorBase>(
                   FilterContext*, const AuthenticatorBase::DoneCallback&));
  MOCK_METHOD2(createOriginAuthenticator,
               std::unique_ptr<AuthenticatorBase>(
                   FilterContext*, const AuthenticatorBase::DoneCallback&));
};

class AuthenticationFilterTest : public testing::Test {
 public:
  AuthenticationFilterTest()
      : request_headers_{{":method", "GET"}, {":path", "/"}} {}
  ~AuthenticationFilterTest() {}

  void SetUp() override {
    filter_.setDecoderFilterCallbacks(decoder_callbacks_);
  }

 protected:
  StrictMock<MockAuthenticationFilter> filter_;
  NiceMock<Http::MockStreamDecoderFilterCallbacks> decoder_callbacks_;
  Http::TestHeaderMapImpl request_headers_;
};

TEST_F(AuthenticationFilterTest, PeerFail) {
  // Peer authentication fail, request should be rejected with 401. No origin
  // authentiation needed.
  EXPECT_CALL(filter_, createPeerAuthenticator(_, _))
      .Times(1)
      .WillOnce(Invoke(createAlwaysFailAuthenticator));
  EXPECT_CALL(decoder_callbacks_, encodeHeaders_(_, _))
      .Times(1)
      .WillOnce(testing::Invoke([](Http::HeaderMap& headers, bool) {
        EXPECT_STREQ("401", headers.Status()->value().c_str());
      }));
  EXPECT_EQ(Http::FilterHeadersStatus::StopIteration,
            filter_.decodeHeaders(request_headers_, true));
  EXPECT_FALSE(
      request_headers_.has(AuthenticationFilter::kOutputHeaderLocation));
}

TEST_F(AuthenticationFilterTest, PeerPassOrginFail) {
  // Peer pass thus origin authentication must be called. Final result should
  // fail as origin authn fails.
  EXPECT_CALL(filter_, createPeerAuthenticator(_, _))
      .Times(1)
      .WillOnce(Invoke(createAlwaysPassAuthenticator));
  EXPECT_CALL(filter_, createOriginAuthenticator(_, _))
      .Times(1)
      .WillOnce(Invoke(createAlwaysFailAuthenticator));
  EXPECT_CALL(decoder_callbacks_, encodeHeaders_(_, _))
      .Times(1)
      .WillOnce(testing::Invoke([](Http::HeaderMap& headers, bool) {
        EXPECT_STREQ("401", headers.Status()->value().c_str());
      }));
  EXPECT_EQ(Http::FilterHeadersStatus::StopIteration,
            filter_.decodeHeaders(request_headers_, true));
  EXPECT_FALSE(
      request_headers_.has(AuthenticationFilter::kOutputHeaderLocation));
}

TEST_F(AuthenticationFilterTest, AllPass) {
  EXPECT_CALL(filter_, createPeerAuthenticator(_, _))
      .Times(1)
      .WillOnce(Invoke(createAlwaysPassAuthenticator));
  EXPECT_CALL(filter_, createOriginAuthenticator(_, _))
      .Times(1)
      .WillOnce(Invoke(createAlwaysPassAuthenticator));
  EXPECT_EQ(Http::FilterHeadersStatus::Continue,
            filter_.decodeHeaders(request_headers_, true));
  Result authn;
  EXPECT_TRUE(authn.ParseFromString(Base64::decode(
      request_headers_.get_(AuthenticationFilter::kOutputHeaderLocation))));
  EXPECT_TRUE(TestUtility::protoEqual(
      TestUtilities::AuthNResultFromString(R"(peer_user: "foo")"), authn));
}

}  // namespace
}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
