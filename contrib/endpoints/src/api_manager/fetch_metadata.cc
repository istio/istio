/*
 * Copyright (C) Extensible Service Proxy Authors
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright
 *    notice, this list of conditions and the following disclaimer in the
 *    documentation and/or other materials provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
 * FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
 * DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
 * OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
 * LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
 * OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
 * SUCH DAMAGE.
 */

#include "src/api_manager/fetch_metadata.h"

#include "include/api_manager/http_request.h"
#include "src/api_manager/auth/lib/auth_token.h"

using ::google::api_manager::utils::Status;
using ::google::protobuf::util::error::Code;

namespace google {
namespace api_manager {

namespace {

// URL path for fetching compute metadata.
const char kComputeMetadata[] = "/computeMetadata/v1/?recursive=true";
// URL path for fetching service-account
const char kMetadataServiceAccountToken[] =
    "/computeMetadata/v1/instance/service-accounts/default/token";
// Initial metadata fetch timeout (1s)
const int kMetadataFetchTimeout = 1000;
// Maximum number of retries to fetch metadata
const int kMetadataFetchRetries = 5;
// External status message for failure to fetch metadata
const char kFailedMetadataFetch[] = "Failed to fetch metadata";
// External status message for failure to fetch service account token
const char kFailedTokenFetch[] = "Failed to fetch service account token";
// External status message for token fetch in progress
const char kFetchingToken[] = "Fetching service account token";
// External status message for token parse failure
const char kFailedTokenParse[] = "Failed to parse access token response";
// Time window (in seconds) before expiration to initiate re-fetch
const char kTokenRefetchWindow = 60;

// Issues a HTTP request to fetch the metadata.
void FetchMetadata(
    context::RequestContext *context, const char *path,
    std::function<void(Status, std::map<std::string, std::string> &&,
                       std::string &&)>
        continuation) {
  std::unique_ptr<HTTPRequest> request(new HTTPRequest(continuation));
  request->set_method("GET")
      .set_url(context->service_context()->metadata_server() + path)
      .set_header("Metadata-Flavor", "Google")
      .set_timeout_ms(kMetadataFetchTimeout)
      .set_max_retries(kMetadataFetchRetries);
  context->service_context()->env()->RunHTTPRequest(std::move(request));
}
}  // namespace

void FetchGceMetadata(std::shared_ptr<context::RequestContext> context,
                      std::function<void(Status)> continuation) {
  if (context->service_context()->metadata_server().empty()) {
    // No need to fetching metadata, metadata server address is not set.
    continuation(Status::OK);
    return;
  }

  auto env = context->service_context()->env();
  switch (context->service_context()->gce_metadata()->state()) {
    case GceMetadata::FETCHED:
      // Already have metadata.
      env->LogDebug("Metadata already available. Fetch skipped.");
      continuation(Status::OK);
      return;
    case GceMetadata::FAILED:
      // Metadata fetch already failed. Permanent failure.
      env->LogDebug("Metadata fetch previously failed. Skipping with error.");
      continuation(Status(Code::INTERNAL, kFailedMetadataFetch));
      return;
    case GceMetadata::FETCHING:
      env->LogDebug("Another request fetching metadata. Duplicate fetch.");
      continuation(Status(Code::UNAVAILABLE, kFailedMetadataFetch));
      return;
    case GceMetadata::NONE:
    default:
      env->LogDebug("Fetching metadata.");
  }

  context->service_context()->gce_metadata()->set_state(GceMetadata::FETCHING);
  FetchMetadata(
      context.get(), kComputeMetadata,
      [context, continuation](Status status, std::map<std::string, std::string>,
                              std::string &&body) {
        // translate status to external status
        if (status.ok()) {
          status =
              context->service_context()->gce_metadata()->ParseFromJson(&body);
        } else {
          status = Status(Code::INTERNAL, kFailedMetadataFetch);
        }

        // update fetching state
        context->service_context()->gce_metadata()->set_state(
            status.ok() ? GceMetadata::FETCHED : GceMetadata::FAILED);

        continuation(status);
      });
}

void FetchServiceAccountToken(std::shared_ptr<context::RequestContext> context,
                              std::function<void(Status status)> continuation) {
  const auto env = context->service_context()->env();
  const auto token = context->service_context()->service_account_token();

  // If metadata server is not configured, skip it
  // If client auth secret is available, skip fetching
  if (context->service_context()->metadata_server().empty() ||
      token->has_client_secret()) {
    continuation(Status::OK);
    return;
  }

  switch (token->state()) {
    case auth::ServiceAccountToken::FETCHED:
      // If token is going to last longer than the window, continue
      if (token->is_access_token_valid(kTokenRefetchWindow)) {
        continuation(Status::OK);
        return;
      }

      // If token is about to expire, initiate fetching a fresh token
      // Expects token to last significantly longer than time lookahead
      token->set_state(auth::ServiceAccountToken::NONE);

      // The first request within the token re-fetch window will carry on
      // the token fetch, while subsequent requests in the window reuse
      // the old token.
      break;
    case auth::ServiceAccountToken::FETCHING:
      env->LogDebug("Service account token fetch in progress");
      // If token is still valid, continue
      if (token->is_access_token_valid(0)) {
        continuation(Status::OK);
      } else {
        continuation(Status(Code::UNAVAILABLE, kFetchingToken));
      }
      return;
    case auth::ServiceAccountToken::FAILED:
      // permanent failure
      continuation(Status(Code::INTERNAL, kFailedTokenFetch));
      return;
    case auth::ServiceAccountToken::NONE:
    default:
      env->LogDebug("Need to fetch service account token");
  }

  token->set_state(auth::ServiceAccountToken::FETCHING);
  FetchMetadata(context.get(), kMetadataServiceAccountToken,
                [env, token, continuation](
                    Status status, std::map<std::string, std::string> &&,
                    std::string &&body) {
                  // fetch failed
                  if (!status.ok()) {
                    env->LogDebug("Failed to fetch service account token");
                    token->set_state(auth::ServiceAccountToken::FAILED);
                    continuation(Status(Code::INTERNAL, kFailedTokenFetch));
                    return;
                  }

                  // process token from the body
                  char *auth_token = nullptr;
                  int expires = 0;
                  if (!auth::esp_get_service_account_auth_token(
                          const_cast<char *>(body.data()), body.length(),
                          &auth_token, &expires) ||
                      token == nullptr) {
                    env->LogDebug("Failed to parse token response body");
                    token->set_state(auth::ServiceAccountToken::FAILED);
                    continuation(Status(Code::INTERNAL, kFailedTokenParse));
                    return;
                  }

                  token->set_state(auth::ServiceAccountToken::FETCHED);
                  // Set expiration time a little bit earlier to avoid rejection
                  // of the actual service control requests.
                  // Even there is a prefetch window of 60 seconds, but prefetch
                  // window may not kick in if not on-going requests.
                  token->set_access_token(auth_token, expires - 50);
                  free(auth_token);

                  continuation(Status::OK);
                });
}
}  // namespace api_manager
}  // namespace google
