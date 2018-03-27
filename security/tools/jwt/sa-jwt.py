# Copyright 2018 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Python script generates a JWT signed by a Google service account"""
import argparse
import time

import google.auth.crypt
import google.auth.jwt


def main(args):
    """Generates a signed JSON Web Token using a Google API Service Account."""
    signer = google.auth.crypt.RSASigner.from_service_account_file(
        args.service_account_file)
    now = int(time.time())
    payload = {
        # expire in one hour.
        "exp": now + 3600,
        "iat": now,
        # Add any custom claims here.
        # e.g.,
        # "email": alice@yahoo.com
    }
    if args.iss:
        payload["iss"] = args.iss

    if args.sub:
        payload["sub"] = args.sub
    else:
        payload["sub"] = args.iss

    if args.aud:
        payload["aud"] = args.aud

    signed_jwt = google.auth.jwt.encode(signer, payload)
    return signed_jwt


if __name__ == '__main__':
    parser = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter)
    # positional arguments
    parser.add_argument(
        'service_account_file',
        help='The path to your service account key file (in JSON format).')
    # optional arguments
    parser.add_argument("-iss", "--iss",
                        help="iss claim. This should be your service account email.")
    parser.add_argument("-aud", "--aud",
                        help="aud claim")
    parser.add_argument("-sub", "--sub",
                        help="sub claim. If not provided, it is set to the same as iss claim.")
    print main(parser.parse_args())
