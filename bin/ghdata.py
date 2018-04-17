#!/usr/bin/env python

import requests
import os
import sys
import datetime

# get_rawdata uses github releases API to fetch latest releases info
# download_counts are only available to admin users


def get_rawdata(token, repo="istio"):
    release = "https://api.github.com/repos/istio/{}/releases".format(repo)
    headers = {"Authorization": "Bearer {}".format(token)}
    resp = requests.get(release, headers=headers)
    if not resp.ok:
        raise Exception(resp.content)

    return resp.json()

# Name of the env var
GITHUB_TOKEN = "GITHUB_TOKEN"


def usage():
    print "Visit https://github.com/settings/tokens to generate a token"
    print "You must have admin access on the repository get download counts"


def main(args):
    token = os.environ.get(GITHUB_TOKEN)
    if token is None and len(args) > 0:
        token = args[0]
        # if 1st arg starts with @ read the file
        if token.startsWith("@"):
            token = open(token).read()

    if token is None:
        print "Unable to get GITHUB_TOKEN as env var, first argument or @file"
        usage()
        return -1

    try:
        data = get_rawdata(token)
    except Exception as ex:
        print ex
        usage()
        return -1

    print "# Report created at UTC:", str(datetime.datetime.utcnow())
    for d in ["{}, {}, {}".format(
            q['created_at'],
            q['download_count'],
            q['browser_download_url'].split('/')[-1])
            for j in data for q in j['assets']]:
        print d

    return 0

if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
