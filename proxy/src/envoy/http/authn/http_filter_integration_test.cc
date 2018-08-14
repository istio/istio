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

#include "common/common/base64.h"
#include "common/common/utility.h"
#include "extensions/filters/http/well_known_names.h"
#include "fmt/printf.h"
#include "proxy/src/envoy/utils/filter_names.h"
#include "proxy/src/istio/authn/context.pb.h"
#include "test/integration/http_protocol_integration.h"

using google::protobuf::util::MessageDifferencer;
using istio::authn::Payload;
using istio::authn::Result;

namespace Envoy {
namespace {

static const Envoy::Http::LowerCaseString kSecIstioAuthnPayloadHeaderKey(
    "sec-istio-authn-payload");

// Default request for testing.
static const Http::TestHeaderMapImpl kSimpleRequestHeader{{
    {":method", "GET"},
    {":path", "/"},
    {":scheme", "http"},
    {":authority", "host"},
    {"x-forwarded-for", "10.0.0.1"},
}};

// Keep the same as issuer in the policy below.
static const char kJwtIssuer[] = "some@issuer";

static const char kAuthnFilterWithJwt[] = R"(
    name: istio_authn
    config:
      policy:
        origins:
        - jwt:
            issuer: some@issuer
            jwks_uri: http://localhost:8081/)";

// Payload data to inject. Note the iss claim intentionally set different from
// kJwtIssuer.
static const char kMockJwtPayload[] =
    "{\"iss\":\"https://example.com\","
    "\"sub\":\"test@example.com\",\"exp\":2001001001,"
    "\"aud\":\"example_service\"}";
// Returns a simple header-to-metadata filter config that can be used to inject
// data into request info dynamic metadata for testing.
std::string MakeHeaderToMetadataConfig() {
  return fmt::sprintf(
      R"(
    name: %s
    config:
      request_rules:
      - header: x-mock-metadata-injection
        on_header_missing:
          metadata_namespace: %s
          key: %s
          value: "%s"
          type: STRING)",
      Extensions::HttpFilters::HttpFilterNames::get().HeaderToMetadata,
      Utils::IstioFilterName::kJwt, kJwtIssuer,
      StringUtil::escape(kMockJwtPayload));
}

typedef HttpProtocolIntegrationTest AuthenticationFilterIntegrationTest;

INSTANTIATE_TEST_CASE_P(
    Protocols, AuthenticationFilterIntegrationTest,
    testing::ValuesIn(HttpProtocolIntegrationTest::getProtocolTestParams()),
    HttpProtocolIntegrationTest::protocolTestParamsToString);

TEST_P(AuthenticationFilterIntegrationTest, EmptyPolicy) {
  config_helper_.addFilter("name: istio_authn");
  initialize();
  codec_client_ =
      makeHttpConnection(makeClientConnection((lookupPort("http"))));
  auto response = codec_client_->makeHeaderOnlyRequest(kSimpleRequestHeader);
  // Wait for request to upstream (backend)
  waitForNextUpstreamRequest();

  // Send backend response.
  upstream_request_->encodeHeaders(Http::TestHeaderMapImpl{{":status", "200"}},
                                   true);

  response->waitForEndStream();
  EXPECT_TRUE(response->complete());
  EXPECT_STREQ("200", response->headers().Status()->value().c_str());
}

TEST_P(AuthenticationFilterIntegrationTest, SourceMTlsFail) {
  config_helper_.addFilter(R"(
    name: istio_authn
    config:
      policy:
        peers:
        - mtls: {})");
  initialize();

  // AuthN filter use MTls, but request doesn't have certificate, request
  // would be rejected.
  codec_client_ =
      makeHttpConnection(makeClientConnection((lookupPort("http"))));
  auto response = codec_client_->makeHeaderOnlyRequest(kSimpleRequestHeader);

  // Request is rejected, there will be no upstream request (thus no
  // waitForNextUpstreamRequest).
  response->waitForEndStream();
  EXPECT_TRUE(response->complete());
  EXPECT_STREQ("401", response->headers().Status()->value().c_str());
}

// TODO (diemtvu/lei-tang): add test for MTls success.

TEST_P(AuthenticationFilterIntegrationTest, OriginJwtRequiredHeaderNoJwtFail) {
  config_helper_.addFilter(kAuthnFilterWithJwt);
  initialize();

  // The AuthN filter requires JWT, but request doesn't have JWT, request
  // would be rejected.
  codec_client_ =
      makeHttpConnection(makeClientConnection((lookupPort("http"))));
  auto response = codec_client_->makeHeaderOnlyRequest(kSimpleRequestHeader);

  // Request is rejected, there will be no upstream request (thus no
  // waitForNextUpstreamRequest).
  response->waitForEndStream();
  EXPECT_TRUE(response->complete());
  EXPECT_STREQ("401", response->headers().Status()->value().c_str());
}

TEST_P(AuthenticationFilterIntegrationTest, CheckValidJwtPassAuthentication) {
  config_helper_.addFilter(kAuthnFilterWithJwt);
  config_helper_.addFilter(MakeHeaderToMetadataConfig());
  initialize();

  // The AuthN filter requires JWT. The http request contains validated JWT and
  // the authentication should succeed.
  codec_client_ =
      makeHttpConnection(makeClientConnection((lookupPort("http"))));
  auto response = codec_client_->makeHeaderOnlyRequest(kSimpleRequestHeader);

  // Wait for request to upstream (backend)
  waitForNextUpstreamRequest();
  // Send backend response.
  upstream_request_->encodeHeaders(Http::TestHeaderMapImpl{{":status", "200"}},
                                   true);

  response->waitForEndStream();
  EXPECT_TRUE(response->complete());
  EXPECT_STREQ("200", response->headers().Status()->value().c_str());
}

}  // namespace
}  // namespace Envoy
