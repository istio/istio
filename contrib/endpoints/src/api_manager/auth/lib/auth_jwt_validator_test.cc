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
#include "src/api_manager/auth/lib/auth_jwt_validator.h"

#include "gtest/gtest.h"
#include "src/api_manager/auth/lib/auth_token.h"

#include <cstring>

namespace google {
namespace api_manager {
namespace auth {

namespace {

// Testing service account.
const char kUserId[] =
    "628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount."
    "com";
const char kAudience[] = "http://myservice.com/myapi";
const char kAuthorizedParty[] = "authorized@party.com";

// Generated via service account, and immediately revoked.
const char kWrongPrivateKey[] =
    "{"
    "\"private_key_id\": \"8f0ed82105b173c7267be7185c91b1d74cf52cfd\","
    "\"private_key\": \"-----BEGIN PRIVATE "
    "KEY-----"
    "\\nMIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQDRHBpr7CcRVvWc\\n4lKS"
    "A+"
    "8Qf+uL6V2O2pctSwRUyN8f+rjgs3RJBt2kBg63zsaVUi0X95YjkgF7NzRl\\nRk4WEZSPty4V+"
    "xCuxzouPfqHv1EwFDvC/pCmtVuYMHih2RSrmpKKISVTCdeNZ+Gk\\nXo2o4fN4FD+i/"
    "FUx+"
    "t5vmUIIjai8UxTHjsqzVEX16CE1aQA6XyqL7subbNyOaMk1\\nhTQsfF3ANm9KgxGZpMeYhaty"
    "n"
    "gi6IdX9vhW2k14wjEHYP1QtJjcesDLlXnN+"
    "1cq3\\na44VFC1YCM0rvSHE5VzQU1LF7VgQs1CnT/"
    "bpDLNvPsYTiGPdnbAnxaxBMGiGMg5m\\n4Ch14eRpAgMBAAECggEAPkulA2nC6cOCQE6cUquhW"
    "M"
    "UDIxdOq/Qq/"
    "W9PxwJglmJX\\nGXnctrS46thzIgcT2gA1NuKnc8lXb6Gulk0vjhuGqpnjvOCiw67OgmAsdqxk"
    "P"
    "3KH\\nqzuzVDbLJrep+G13XvgZl9TwDaDs+k9sRU913E4T/"
    "j3qB2As8UrPYWfC6FFrZ06+\\n7KrT6iZQi9eQtqCxOA/"
    "qVHyVsRPBcPEAc3SDahBOyrub7U60O1UVGpZC3ZROJ3X/\\nK1vVWZVBrAnn/TIJ/"
    "gklTZX7nIf9u+uZK0vyb3WXZdDQ+/JlkFL0/"
    "IcV+ocsQYJs\\nCU3cJduTej0YEpCsnwz3vVRrUBl1GmWmfUdI2o/qUQKBgQDyYvRk/"
    "etdfqazAcqI\\n1J1n4PJRpDhwArDVw5+"
    "SYUkcb85WK8UiouK8ckmq8m0BYTIb7E2pzYgin5ggtvwY\\npwIh/"
    "m0K3L7bzCuNnQHetYyQsuD+dqdzKw7aGilpvPARZlF7pC33WrJU5YP/jbNu\\n32iK/"
    "iIpUj+Tizcqs1KwckWxrwKBgQDc2rGSYbm/"
    "ebgdFn3l5WatycmgIE4koUuT\\nF2k9Uk+"
    "DdsRstepxHSIEpm0gfwxsXaeg4AvDldOltFybIuoUw4RIBHlfR/xy4WEB\\nsZtHRWj/QXLS/"
    "4RT/LIy/zkjzvdq82hQvcV14hBmVDmQqbJaEH2/2tv9ZK1QtTAP\\ndRA/"
    "MDvJZwKBgBKQB31wgMTxPRz6ZyNhfQiGjqg39maFnjtQtvjD4JB/"
    "84Jf6cIE\\nTW73JbMky7pOUkMXLr9xURqttD3VJatRpvUpgfpR+3/ju/"
    "Ylbw46QyCVwmtadOp6\\nArIrTL6fTJdYiab5ZNfLp1qfFSPOG07DZ0M1wTH+"
    "7YWEJN5tS0jeB35bAoGBAKXs\\ns94LB7dAJj/"
    "MRxfySjsk0BM6UhsZByNiQlGsxko5b4dRAOqsfYNK2c/BQ78iea7W\\nxF/"
    "T76edorl2+LBS184XdmxMM/"
    "DHPM899TANiL3FGRRGnc9PmT3RG8e4VZAHgQaw\\nHGrdRX7rpjf2FiWuIBuEvSRZgBCTn6DtT"
    "S"
    "B8B17fAoGBALaWesLP4Kfwd27jeqhq\\nHma90f5qB8mndTexPE5sekETUsdXLwsouLX3U94wW"
    "V"
    "1cyd9sg56EScnrqUhyYRWf\\nxXyHgmWDIO5xJxUbduII/"
    "oytxpKSo5CL6ZZZapYS4rasF800LUGZhfCqMO50xAcH\\nauL/"
    "1ayCE32Uwdo5Ws7rU8PE\\n-----END PRIVATE KEY-----\\n\","
    "\"client_email\": "
    "\"628645741881-ff881mb5f6n4onn1d4hl4e9vb87ecl1e@developer.gserviceaccount."
    "com\","
    "\"client_id\": "
    "\"628645741881-ff881mb5f6n4onn1d4hl4e9vb87ecl1e.apps.googleusercontent."
    "com\","
    "\"type\": \"service_account\""
    "}";

const char kOkPrivateKey[] =
    "{\"private_key_id\": "
    "\"b3319a147514df7ee5e4bcdee51350cc890cc89e\",\"private_key\": "
    "\"-----BEGIN PRIVATE "
    "KEY-----"
    "\\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQCoOLtPHgOE289C\\nyXWh"
    "/HFzZ49AVyz4vSZdijpMZLrgJj/ZaY629iVws1mOG511lVXZfzybQx/"
    "BpIDX\\nrAT5GIoz2GqjkRjwE9ePnsIyJgDKIe5A+nXJrKMyCgTU/"
    "aO+"
    "nh6oX4FOKWUYm3lb\\nlG5e2L26p8y0JB1qAHwQLcw1G5T8p14uAHLeVLeijgs5h37viREFVlu"
    "TbCeaZvsi\\nE/"
    "06gtzX7v72pTW6GkPGYTonAFq7SYNLAydgNLgb8wvXt0L5kO0t3WLbhJNTDf0o\\nfSlxJ18Vs"
    "vY20Rl015qbUMN2TSJS0lI9mWJQckEj+mPwz7Yyf+"
    "gDyMG4jxgrAGpi\\nRkI3Uj3lAgMBAAECggEAOuaaVyp4KvXYDVeC07QTeUgCdZHQkkuQemIi5"
    "YrDkCZ0\\nZsi6CsAG/f4eVk6/"
    "BGPEioItk2OeY+wYnOuDVkDMazjUpe7xH2ajLIt3DZ4W2q+"
    "k\\nv6WyxmmnPqcZaAZjZiPxMh02pkqCNmqBxJolRxp23DtSxqR6lBoVVojinpnIwem6\\nxyU"
    "l65u0mvlluMLCbKeGW/"
    "K9bGxT+"
    "qd3qWtYFLo5C3qQscXH4L0m96AjGgHUYW6M\\nFfs94ETNfHjqICbyvXOklabSVYenXVRL24TO"
    "KIHWkywhi1wW+"
    "Q6zHDADSdDVYw5l\\nDaXz7nMzJ2X7cuRP9zrPpxByCYUZeJDqej0Pi7h7ZQKBgQDdI7Yb3xFX"
    "pbuPd1VS\\ntNMltMKzEp5uQ7FXyDNI6C8+"
    "9TrjNMduTQ3REGqEcfdWA79FTJq95IM7RjXX9Aae\\np6cLekyH8MDH/"
    "SI744vCedkD2bjpA6MNQrzNkaubzGJgzNiZhjIAqnDAD3ljHI61\\nNbADc32SQMejb6zlEh8h"
    "ssSsXwKBgQDCvXhTIO/EuE/y5Kyb/"
    "4RGMtVaQ2cpPCoB\\nGPASbEAHcsRk+4E7RtaoDQC1cBRy+"
    "zmiHUA9iI9XZyqD2xwwM89fzqMj5Yhgukvo\\nXMxvMh8NrTneK9q3/"
    "M3mV1AVg71FJQ2oBr8KOXSEbnF25V6/ara2+EpH2C2GDMAo\\npgEnZ0/"
    "8OwKBgFB58IoQEdWdwLYjLW/"
    "d0oGEWN6mRfXGuMFDYDaGGLuGrxmEWZdw\\nfzi4CquMdgBdeLwVdrLoeEGX+XxPmCEgzg/"
    "FQBiwqtec7VpyIqhxg2J9V2elJS9s\\nPB1rh9I4/QxRP/"
    "oO9h9753BdsUU6XUzg7t8ypl4VKRH3UCpFAANZdW1tAoGAK4ad\\ntjbOYHGxrOBflB5wOiByf"
    "1JBZH4GBWjFf9iiFwgXzVpJcC5NHBKL7gG3EFwGba2M\\nBjTXlPmCDyaSDlQGLavJ2uQar0P0"
    "Y2MabmANgMkO/hFfOXBPtQQe6jAfxayaeMvJ\\nN0fQOylUQvbRTodTf2HPeG9g/"
    "W0sJem0qFH3FrECgYEAnwixjpd1Zm/diJuP0+Lb\\nYUzDP+Afy78IP3mXlbaQ/"
    "RVd7fJzMx6HOc8s4rQo1m0Y84Ztot0vwm9+S54mxVSo\\n6tvh9q0D7VLDgf+"
    "2NpnrDW7eMB3n0SrLJ83Mjc5rZ+wv7m033EPaWSr/TFtc/"
    "MaF\\naOI20MEe3be96HHuWD3lTK0\u003d\\n-----END PRIVATE "
    "KEY-----\\n\",\"client_email\": "
    "\"628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount."
    "com\",\"client_id\": "
    "\"628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l.apps.googleusercontent."
    "com\",\"type\": \"service_account\"}";

const char kPublicKeyJwk[] =
    "{\"keys\": [{\"kty\": \"RSA\",\"alg\": \"RS256\",\"use\": "
    "\"sig\",\"kid\": \"62a93512c9ee4c7f8067b5a216dade2763d32a47\",\"n\": "
    "\"0YWnm_eplO9BFtXszMRQNL5UtZ8HJdTH2jK7vjs4XdLkPW7YBkkm_"
    "2xNgcaVpkW0VT2l4mU3KftR-6s3Oa5Rnz5BrWEUkCTVVolR7VYksfqIB2I_"
    "x5yZHdOiomMTcm3DheUUCgbJRv5OKRnNqszA4xHn3tA3Ry8VO3X7BgKZYAUh9fyZTFLlkeAh0-"
    "bLK5zvqCmKW5QgDIXSxUTJxPjZCgfx1vmAfGqaJb-"
    "nvmrORXQ6L284c73DUL7mnt6wj3H6tVqPKA27j56N0TB1Hfx4ja6Slr8S4EB3F1luYhATa1PKU"
    "SH8mYDW11HolzZmTQpRoLV8ZoHbHEaTfqX_aYahIw\",\"e\": \"AQAB\"},{\"kty\": "
    "\"RSA\",\"alg\": \"RS256\",\"use\": \"sig\",\"kid\": "
    "\"b3319a147514df7ee5e4bcdee51350cc890cc89e\",\"n\": "
    "\"qDi7Tx4DhNvPQsl1ofxxc2ePQFcs-L0mXYo6TGS64CY_"
    "2WmOtvYlcLNZjhuddZVV2X88m0MfwaSA16wE-"
    "RiKM9hqo5EY8BPXj57CMiYAyiHuQPp1yayjMgoE1P2jvp4eqF-"
    "BTillGJt5W5RuXti9uqfMtCQdagB8EC3MNRuU_KdeLgBy3lS3oo4LOYd-"
    "74kRBVZbk2wnmmb7IhP9OoLc1-7-9qU1uhpDxmE6JwBau0mDSwMnYDS4G_ML17dC-"
    "ZDtLd1i24STUw39KH0pcSdfFbL2NtEZdNeam1DDdk0iUtJSPZliUHJBI_pj8M-2Mn_"
    "oA8jBuI8YKwBqYkZCN1I95Q\",\"e\": \"AQAB\"}]}";

const char kPublicKeyX509[] =
    "{\"62a93512c9ee4c7f8067b5a216dade2763d32a47\": \"-----BEGIN "
    "CERTIFICATE-----"
    "\\nMIIDYDCCAkigAwIBAgIIEzRv3yOFGvcwDQYJKoZIhvcNAQEFBQAwUzFRME8GA1UE\\nAxNI"
    "NjI4NjQ1NzQxODgxLW5vYWJpdTIzZjVhOG04b3ZkOHVjdjY5OGxqNzh2djBs\\nLmFwcHMuZ29"
    "vZ2xldXNlcmNvbnRlbnQuY29tMB4XDTE1MDkxMTIzNDg0OVoXDTI1\\nMDkwODIzNDg0OVowUz"
    "FRME8GA1UEAxNINjI4NjQ1NzQxODgxLW5vYWJpdTIzZjVh\\nOG04b3ZkOHVjdjY5OGxqNzh2d"
    "jBsLmFwcHMuZ29vZ2xldXNlcmNvbnRlbnQuY29t\\nMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A"
    "MIIBCgKCAQEA0YWnm/eplO9BFtXszMRQ\\nNL5UtZ8HJdTH2jK7vjs4XdLkPW7YBkkm/"
    "2xNgcaVpkW0VT2l4mU3KftR+6s3Oa5R\\nnz5BrWEUkCTVVolR7VYksfqIB2I/"
    "x5yZHdOiomMTcm3DheUUCgbJRv5OKRnNqszA\\n4xHn3tA3Ry8VO3X7BgKZYAUh9fyZTFLlkeA"
    "h0+bLK5zvqCmKW5QgDIXSxUTJxPjZ\\nCgfx1vmAfGqaJb+"
    "nvmrORXQ6L284c73DUL7mnt6wj3H6tVqPKA27j56N0TB1Hfx4\\nja6Slr8S4EB3F1luYhATa1"
    "PKUSH8mYDW11HolzZmTQpRoLV8ZoHbHEaTfqX/"
    "aYah\\nIwIDAQABozgwNjAMBgNVHRMBAf8EAjAAMA4GA1UdDwEB/"
    "wQEAwIHgDAWBgNVHSUB\\nAf8EDDAKBggrBgEFBQcDAjANBgkqhkiG9w0BAQUFAAOCAQEAP4gk"
    "DCrPMI27/"
    "QdN\\nwW0mUSFeDuM8VOIdxu6d8kTHZiGa2h6nTz5E+"
    "twCdUuo6elGit3i5H93kFoaTpex\\nj/eDNoULdrzh+cxNAbYXd8XgDx788/"
    "jm06qkwXd0I5s9KtzDo7xxuBCyGea2LlpM\\n2HOI4qFunjPjFX5EFdaT/Rh+qafepTKrF/"
    "GQ7eGfWoFPbZ29Hs5y5zATJCDkstkY\\npnAya8O8I+"
    "tfKjOkcra9nOhtck8BK94tm3bHPdL0OoqKynnoRCJzN5KPlSGqR/h9\\nSMBZzGtDOzA2sX/"
    "8eyU6Rm4MV6/1/53+J6EIyarR5g3IK1dWmz/YT/YMCt6LhHTo\\n3yfXqQ==\\n-----END "
    "CERTIFICATE-----\\n\",\"b3319a147514df7ee5e4bcdee51350cc890cc89e\": "
    "\"-----BEGIN "
    "CERTIFICATE-----"
    "\\nMIIDYDCCAkigAwIBAgIICjE9gZxAlu8wDQYJKoZIhvcNAQEFBQAwUzFRME8GA1UE\\nAxNI"
    "NjI4NjQ1NzQxODgxLW5vYWJpdTIzZjVhOG04b3ZkOHVjdjY5OGxqNzh2djBs\\nLmFwcHMuZ29"
    "vZ2xldXNlcmNvbnRlbnQuY29tMB4XDTE1MDkxMzAwNTAyM1oXDTI1\\nMDkxMDAwNTAyM1owUz"
    "FRME8GA1UEAxNINjI4NjQ1NzQxODgxLW5vYWJpdTIzZjVh\\nOG04b3ZkOHVjdjY5OGxqNzh2d"
    "jBsLmFwcHMuZ29vZ2xldXNlcmNvbnRlbnQuY29t\\nMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A"
    "MIIBCgKCAQEAqDi7Tx4DhNvPQsl1ofxx\\nc2ePQFcs+L0mXYo6TGS64CY/"
    "2WmOtvYlcLNZjhuddZVV2X88m0MfwaSA16wE+"
    "RiK\\nM9hqo5EY8BPXj57CMiYAyiHuQPp1yayjMgoE1P2jvp4eqF+"
    "BTillGJt5W5RuXti9\\nuqfMtCQdagB8EC3MNRuU/"
    "KdeLgBy3lS3oo4LOYd+74kRBVZbk2wnmmb7IhP9OoLc\\n1+7+"
    "9qU1uhpDxmE6JwBau0mDSwMnYDS4G/"
    "ML17dC+ZDtLd1i24STUw39KH0pcSdf\\nFbL2NtEZdNeam1DDdk0iUtJSPZliUHJBI/"
    "pj8M+2Mn/"
    "oA8jBuI8YKwBqYkZCN1I9\\n5QIDAQABozgwNjAMBgNVHRMBAf8EAjAAMA4GA1UdDwEB/"
    "wQEAwIHgDAWBgNVHSUB\\nAf8EDDAKBggrBgEFBQcDAjANBgkqhkiG9w0BAQUFAAOCAQEAHSPR"
    "7fDAWyZ825IZ\\n86hEsQZCvmC0QbSzy62XisM/uHUO75BRFIAvC+zZAePCcNo/"
    "nh6FtEM19wZpxLiK\\n0m2nqDMpRdw3Qt6BNhjJMozTxA2Xdipnfq+fGpa+"
    "bMkVpnRZ53qAuwQpaKX6vagr\\nj83Bdx2b5WPQCg6xrQWsf79Vjj2U1hdw7+"
    "klcF7tLef1p8qA/ezcNXmcZ4BpbpaO\\nN9M4/kQOA3Y2F3ISAaOJzCB25F259whjW+Uuqd/"
    "L9Lb4gPPSUMSKy7Zy4Sn4il1U\\nFc94Mi9j13oeGvLOduNOStGu5XROIxDtCEjjn2y2SL2bPw"
    "0qAlIzBeniiApkmYw/\\no6OLrg==\\n-----END CERTIFICATE-----\\n\"}";

// Token generated with the following header and payload and kOkPrivateKey.
// Header (kid is not specified):
// {
//   "alg": "RS256",
//   "typ": "JWT"
// }
// Payload:
// {
//   "iss": "628645741881-"
//     "noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com",
//   "sub": "628645741881-"
//     "noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com",
//   "aud": "http://myservice.com/myapi"
// }
const char kTokenNoKid[] =
    "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiI2Mjg2NDU3NDE4ODEtbm9hYml1M"
    "jNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MGxAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb20"
    "iLCJzdWIiOiI2Mjg2NDU3NDE4ODEtbm9hYml1MjNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MGxAZ"
    "GV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb20iLCJhdWQiOiJodHRwOi8vbXlzZXJ2aWNlLmN"
    "vbS9teWFwaSJ9.gq_4ucjddQDjYK5FJr_kXmMo2fgSEB6Js1zopcQLVpCKFDNb-TQ97go0wuk5"
    "_vlSp_8I2ImrcdwYbAKqYCzcdyBXkAYoHCGgmY-v6MwZFUvrIaDzR_M3rmY8sQ8cdN3MN6ZRbB"
    "6opHwDP1lUEx4bZn_ZBjJMPgqbIqGmhoT1UpfPF6P1eI7sXYru-4KVna0STOynLl3d7JYb7E-8"
    "ifcjUJLhat8JR4zR8i4-zWjn6d6j_NI7ZvMROnao77D9YyhXv56zfsXRatKzzYtxPlQMz4AjP-"
    "bUHfbHmhiIOOAeEKFuIVUAwM17j54M6VQ5jnAabY5O-ermLfwPiXvNt2L2SA==";

// Token that has one dot.
const char kTokenOneDot[] =
    "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9."
    "eyJpc3MiOiI2Mjg2NDU3NDE4ODEtbm9hYml1M";

// Token that does not have signature part.
const char kTokenNoSign[] =
    "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiI2Mjg2NDU3NDE4ODEtbm9hYml1M"
    "jNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MGxAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb20"
    "iLCJzdWIiOiI2Mjg2NDU3NDE4ODEtbm9hYml1MjNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MGxAZ"
    "GV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb20iLCJhdWQiOiJodHRwOi8vbXlzZXJ2aWNlLmN"
    "vbS9teWFwaSJ9.";

// Token without "iss" in payload.
const char kTokenNoIss[] =
    "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiI2Mjg2NDU3NDE4ODEtbm9hYml1"
    "MjNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MGxAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb"
    "20iLCJhdWQiOiJodHRwOi8vbXlzZXJ2aWNlLmNvbS9teWFwaSIsImV4cCI6MjQ2MjMyNDAyMH"
    "0=.m08o3yaV3Xhth0MdI8TviotIbew1TcBfepYMtAbwSzBsSFtTXUCpmyph4ld-ji7koSXipx"
    "Sssq43qqSX_ywOBq4mfChmDZL-DLvJlGnIkF_ec2vn9FgOUFivPGcrPSkyOBcKGPj4JA764oO"
    "Sg1VMN34sB9qc57qQHQBL3SYv1exyPOZksf9Y-UTLdbZJp0ACDeqEbvrOKhkylUrCtMtJZe0l"
    "b8h4sSgZ5fiNev4eyX5M8WB6dCFmehAUabYop8_3XSy1Ufxo_g-Is9CXKStSOWxRlea7o_rNO"
    "P1yrXjS-A7T-fCvqrEYYn4bZh7gYh4Q_Gu0SHlzwviYFzcfLpti7A==";

// Token without "sub" in payload.
const char kTokenNoSub[] =
    "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiI2Mjg2NDU3NDE4ODEtbm9hYml1"
    "MjNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MGxAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb"
    "20iLCJhdWQiOiJodHRwOi8vbXlzZXJ2aWNlLmNvbS9teWFwaSIsImV4cCI6MjQ2MjMyNDAyMH"
    "0=.Rq5UHXlRxzNvklEacP9v2YfwoztSTdhN0A5ecGrH06mfqvsmqZ6OEHsTDUIFUE85BkSA71"
    "vjDhkMQYxi2yn8TaJ9ELYFKggfIhuHX5BGF5PgdMZnIR3JDHDXQmOBLwGtBPtC0VUCbDnzG_P"
    "j5Syd4yU0_4FbIl8B2iw6rlxER1JL38Hm7K4pFUPhEgTM1suM_bzMRPLOmInw4neJDhuZOaG0"
    "7d__D1Z8Z6QCiXTEEMTjswT8cYxzeLp8EOdqKdiHIn6ynawJozjha7NS1GxA5-M2anQnzLf1y"
    "h2Uc9Cg5Kx0PgYzjk5GkoWWl8zOCekgvzyLFNSttO3013WtUlw7dA==";

// Token without "aud" in payload.
const char kTokenNoAud[] =
    "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiI2Mjg2NDU3NDE4ODEtbm9hYml1"
    "MjNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MGxAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb"
    "20iLCJzdWIiOiI2Mjg2NDU3NDE4ODEtbm9hYml1MjNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MG"
    "xAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb20iLCJleHAiOjI0NjIzMjQwMjB9.IlOJAg"
    "rpgO9q6A3D9uRqNQ_NwU83PuZn-YaB5LqGfLvAL6bd-JPuwN_RRlWRtj6SPowm7MGXpEw8PvR"
    "vj1IV8RRhLzSfr_gA_6gAiYpRs-_wD2FGCl3wzhnBL387j9m8M04xUdt2-a1WfXBgPWpV-rZa"
    "Q_jqi_pad26AXZtW-iit7fy2K7viH2PKxVQgFjjjtqTuLNoFJjH1fTd3tGZr0ynF4UcUjRWWg"
    "QROJ9A3YNRSGM3ejfsXGCFUuREJ8G7X7dxukd19fnuX4u6iPziXqqFWtaJoaBYKkX4SQ254bl"
    "CKvuNLFiokcJGxGyZ6STLUCB5cBvlQjzfD3ZSAUBKGtA==";

// Token with multiple audiences.
const char kTokenMultiAud[] =
    "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiI2Mjg2NDU3NDE4ODEtbm9hYml1"
    "MjNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MGxAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb"
    "20iLCJzdWIiOiI2Mjg2NDU3NDE4ODEtbm9hYml1MjNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MG"
    "xAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb20iLCJhdWQiOlsiaHR0cDovL215c2Vydml"
    "jZS5jb20vbXlhcGkiLCJodHRwczovL2FjY291bnRzLmdvb2dsZS5jb20iXSwiZXhwIjoyNDYy"
    "MzI0MDIwfQ==.Arx-W6LPwK74u9Wndg85TPGDGuJysyGe3ApqcA905kR0fzKYS8sH8lKdzbk6"
    "xGYS1VMNIWpTrjM9Nf28ZB_r-1j_iYfjS4VREAbFlv4MGionYQDI7eDIpeh-CqeKFs3yPRJHV"
    "nviZ5wRE-16qT3wvxdib3oIimWrnq6MVLL6WTXvz4OLTAJr74Oak88fF48KcCZKQ9Ffg1DF81"
    "qGuAjFES_t6LmmSgi31nI5R6frO98k0DQgAv2m16qC2CidhzStwFIaFVYWBaOwkoB-RQaH8Zr"
    "wgOlvF_7CgLuva1bhuYfDDc8jjgqU-vGKdctc87KK4tkc_OQiwe-RLH6o8sF5vw==";

// Token with the "azp" claim.
// Payload:
// {
//   "iss": "628645741881-"
//     "noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com",
//   "sub": "628645741881-"
//     "noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com",
//   "aud": "http://myservice.com/myapi",
//   "azp": "authorized@party.com"
// }
const char kTokenWithAzp[] =
    "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiI2Mjg2NDU3NDE4ODEtbm9hYml1"
    "MjNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MGxAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb"
    "20iLCJzdWIiOiI2Mjg2NDU3NDE4ODEtbm9hYml1MjNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MG"
    "xAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb20iLCJhdWQiOiJodHRwOi8vbXlzZXJ2aWN"
    "lLmNvbS9teWFwaSIsImF6cCI6ImF1dGhvcml6ZWRAcGFydHkuY29tIn0.I48qxD87l2k6UBw-"
    "1NiP-JgBAyelOFSs3_dUlEeyvdwkeahcgyHBu7W8lOdESHci88uBof-6DjLgIJGQv0-9xGRuY"
    "EfnBDtUkPjVDk_9NCipoi2X2HVrcLYY0AJFQnd57UL3bqDudY8-lx4QXxsczNMba6eyInibdw"
    "B3VAsgcZZAqMGu91i-d12ayNodrKurCGY7tH_9bf7kxhtcB-YDAepLnaLZ4pOjTZe4Ap20G2p"
    "Z_Wbu2Pc9Pq0kPHQo-e9gKCt403cI8MaVxDQkSolpjiVg29rul5m7k359q_XVexvsboHRVP2-"
    "no5Y_Ge3KbA7XosymMYlal0J0iYHQuV_sw";

class JwtValidatorTest : public ::testing::Test {
 public:
  void SetUp() {}
};

// Test JwtValidator class for a given JWT (token) and public key (pkey).
void TestTokenWithPubkey(char *token, const char *pkey) {
  ASSERT_TRUE(token != nullptr);
  UserInfo user_info;

  std::unique_ptr<JwtValidator> validator =
      JwtValidator::Create(token, strlen(token));
  Status status = validator->Parse(&user_info);
  ASSERT_TRUE(status.ok());
  ASSERT_EQ(status.message(), "");
  ASSERT_EQ(kUserId, user_info.id);
  ASSERT_TRUE(user_info.audiences.end() != user_info.audiences.find(kAudience));
  ASSERT_EQ(1U, user_info.audiences.size());
  ASSERT_EQ(kUserId, user_info.issuer);

  status = validator->VerifySignature(pkey, strlen(pkey));
  ASSERT_TRUE(status.ok());
  ASSERT_EQ(status.message(), "");

  // Wrong length.
  validator = JwtValidator::Create(token + 1, strlen(token));
  status = validator->Parse(&user_info);
  ASSERT_FALSE(status.ok());
  ASSERT_EQ(status.message(), "BAD_FORMAT") << status.message();

  // Wrong sig.
  validator = JwtValidator::Create(token, strlen(token) - 1);
  status = validator->Parse(&user_info);
  ASSERT_TRUE(status.ok());
  status = validator->VerifySignature(pkey, strlen(pkey));
  ASSERT_FALSE(status.ok());
  ASSERT_EQ(status.message(), "BAD_SIGNATURE") << status.message();

  // Wrong key length.
  validator = JwtValidator::Create(token, strlen(token));
  status = validator->Parse(&user_info);
  ASSERT_TRUE(status.ok());
  status = validator->VerifySignature(pkey, strlen(pkey) - 1);
  ASSERT_FALSE(status.ok());
  ASSERT_EQ(status.message(), "KEY_RETRIEVAL_ERROR") << status.message();

  // Empty key.
  validator = JwtValidator::Create(token, strlen(token));
  status = validator->Parse(&user_info);
  ASSERT_TRUE(status.ok());
  status = validator->VerifySignature("", 0);
  ASSERT_FALSE(status.ok());
  ASSERT_EQ(status.message(), "BAD_FORMAT") << status.message();
}

TEST_F(JwtValidatorTest, OkTokenX509) {
  // TODO: update esp_get_auth_token to generate token with multiple
  // audiences.
  char *token = esp_get_auth_token(kOkPrivateKey, kAudience);
  TestTokenWithPubkey(token, kPublicKeyX509);

  esp_grpc_free(token);
}

TEST_F(JwtValidatorTest, OkTokenJwk) {
  char *token = esp_get_auth_token(kOkPrivateKey, kAudience);
  TestTokenWithPubkey(token, kPublicKeyJwk);

  esp_grpc_free(token);
}

// This test uses a JWT with no "kid" specided in its header.
TEST_F(JwtValidatorTest, TokenNoKid) {
  // Use public keys of JWK format to verify token signature.
  TestTokenWithPubkey(const_cast<char *>(kTokenNoKid), kPublicKeyJwk);

  // Use public keys of X509 format to verify token signature.
  TestTokenWithPubkey(const_cast<char *>(kTokenNoKid), kPublicKeyX509);
}

TEST_F(JwtValidatorTest, ParseToken) {
  // Multiple audiences.
  UserInfo user_info;
  std::unique_ptr<JwtValidator> validator =
      JwtValidator::Create(kTokenMultiAud, strlen(kTokenMultiAud));
  Status status = validator->Parse(&user_info);
  ASSERT_TRUE(status.ok());
  ASSERT_EQ(user_info.audiences.size(), 2U);

  // Token with only one dot.
  const char *token = "a1234.b5678";  // should have 2 dots.
  validator = JwtValidator::Create(token, strlen(token));
  status = validator->Parse(&user_info);
  ASSERT_FALSE(status.ok());
  ASSERT_EQ(status.message(), "BAD_FORMAT") << status.message();

  // Token with three dots.
  token = "a1234.b5678.c7890.d1234";  // should have 2 dots.
  validator = JwtValidator::Create(token, strlen(token));
  status = validator->Parse(&user_info);
  ASSERT_FALSE(status.ok());
  ASSERT_EQ(status.message(), "BAD_FORMAT") << status.message();

  // Token without dot.
  token = "a1234";  // should have 2 dots.
  validator = JwtValidator::Create(token, strlen(token));
  status = validator->Parse(&user_info);
  ASSERT_FALSE(status.ok());
  ASSERT_EQ(status.message(), "BAD_FORMAT") << status.message();

  // Empty token.
  token = "";  // should have 2 dots.
  validator = JwtValidator::Create(token, strlen(token));
  status = validator->Parse(&user_info);
  ASSERT_FALSE(status.ok());
  ASSERT_EQ(status.message(), "BAD_FORMAT") << status.message();

  // Token truncated in the second part.
  validator = JwtValidator::Create(kTokenOneDot, strlen(kTokenOneDot));
  status = validator->Parse(&user_info);
  ASSERT_FALSE(status.ok());
  ASSERT_EQ(status.message(), "BAD_FORMAT") << status.message();

  // Token without the last signature part.
  validator = JwtValidator::Create(kTokenNoSign, strlen(kTokenNoSign));
  status = validator->Parse(&user_info);
  ASSERT_FALSE(status.ok());
  ASSERT_EQ(status.message(), "BAD_FORMAT") << status.message();

  // Token without "iss" field in payload.
  validator = JwtValidator::Create(kTokenNoIss, strlen(kTokenNoIss));
  status = validator->Parse(&user_info);
  ASSERT_FALSE(status.ok());
  ASSERT_EQ(status.message(), "BAD_FORMAT") << status.message();

  // Token without "sub" field in payload.
  validator = JwtValidator::Create(kTokenNoSub, strlen(kTokenNoSub));
  status = validator->Parse(&user_info);
  ASSERT_FALSE(status.ok());
  ASSERT_EQ(status.message(), "BAD_FORMAT") << status.message();

  // Token without "aud" field in payload.
  validator = JwtValidator::Create(kTokenNoAud, strlen(kTokenNoAud));
  status = validator->Parse(&user_info);
  ASSERT_FALSE(status.ok());
  ASSERT_EQ(status.message(), "BAD_FORMAT") << status.message();
}

TEST_F(JwtValidatorTest, WrongKey) {
  char *token = esp_get_auth_token(kWrongPrivateKey, kAudience);
  ASSERT_TRUE(token != nullptr);
  UserInfo user_info;

  std::unique_ptr<JwtValidator> validator =
      JwtValidator::Create(token, strlen(token));
  Status status = validator->Parse(&user_info);
  ASSERT_TRUE(status.ok());

  status = validator->VerifySignature(kPublicKeyX509, strlen(kPublicKeyX509));
  ASSERT_FALSE(status.ok());
  ASSERT_EQ(status.message(), "KEY_RETRIEVAL_ERROR") << status.message();

  status = validator->VerifySignature(kPublicKeyJwk, strlen(kPublicKeyJwk));
  ASSERT_FALSE(status.ok());
  ASSERT_EQ(status.message(), "KEY_RETRIEVAL_ERROR") << status.message();
}

TEST_F(JwtValidatorTest, TokenWithAuthorizedParty) {
  UserInfo user_info;

  // Token without "azp" claim.
  std::unique_ptr<JwtValidator> validator =
      JwtValidator::Create(kTokenNoKid, strlen(kTokenNoKid));
  Status status = validator->Parse(&user_info);
  ASSERT_TRUE(status.ok());
  ASSERT_TRUE(user_info.authorized_party.empty());

  // Token with "azp" claim.
  validator = JwtValidator::Create(kTokenWithAzp, strlen(kTokenWithAzp));
  status = validator->Parse(&user_info);
  ASSERT_TRUE(status.ok());
  ASSERT_EQ(user_info.authorized_party, kAuthorizedParty);
}

}  // namespace

}  // namespace auth
}  // namespace api_manager
}  // namespace google
