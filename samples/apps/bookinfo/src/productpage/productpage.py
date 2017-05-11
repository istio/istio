#!/usr/bin/python
#
# Copyright 2017 Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.


from flask import Flask, request, render_template, redirect, url_for
import simplejson as json
import requests
import sys
from json2html import *
import logging
import requests

# These two lines enable debugging at httplib level (requests->urllib3->http.client)
# You will see the REQUEST, including HEADERS and DATA, and RESPONSE with HEADERS but without DATA.
# The only thing missing will be the response.body which is not logged.
try:
    import http.client as http_client
except ImportError:
    # Python 2
    import httplib as http_client
http_client.HTTPConnection.debuglevel = 1

app = Flask(__name__)
logging.basicConfig(filename='microservice.log',filemode='w',level=logging.DEBUG)
requests_log = logging.getLogger("requests.packages.urllib3")
requests_log.setLevel(logging.DEBUG)
requests_log.propagate = True
app.logger.addHandler(logging.StreamHandler(sys.stdout))
app.logger.setLevel(logging.DEBUG)

from flask_bootstrap import Bootstrap
Bootstrap(app)

details = {
    "name" : "http://details:9080",
    "endpoint" : "details",
    "children" : []
}

ratings = {
    "name" : "http://ratings:9080",
    "endpoint" : "ratings",
    "children" : []
}

reviews = {
    "name" : "http://reviews:9080",
    "endpoint" : "reviews",
    "children" : [ratings]
}

productpage = {
    "name" : "http://productpage:9080",
    "endpoint" : "details",
    "children" : [details, reviews]
}

service_dict = {
    "productpage" : productpage,
    "details" : details,
    "reviews" : reviews,
}

def getForwardHeaders(request):
    headers = {}

    user_cookie = request.cookies.get("user")
    if user_cookie:
        headers['Cookie'] = 'user=' + user_cookie

    incoming_headers = [ 'x-request-id',
                         'x-b3-traceid',
                         'x-b3-spanid',
                         'x-b3-parentspanid',
                         'x-b3-sampled',
                         'x-b3-flags',
                         'x-ot-span-context'
    ]

    for ihdr in incoming_headers:
        val = request.headers.get(ihdr)
        if val is not None:
            headers[ihdr] = val
            #print "incoming: "+ihdr+":"+val

    return headers

@app.route('/')
@app.route('/index.html')
def index():
    """ Display productpage with normal user and test user buttons"""
    global productpage

    table = json2html.convert(json = json.dumps(productpage),
                              table_attributes="class=\"table table-condensed table-bordered table-hover\"")

    return render_template('index.html', serviceTable=table)

@app.route('/health')
def health():
    return 'Product page is healthy'

@app.route('/login', methods=['POST'])
def login():
    user = request.values.get('username')
    response = app.make_response(redirect(request.referrer))
    response.set_cookie('user', user)
    return response

@app.route('/logout', methods=['GET'])
def logout():
    response = app.make_response(redirect(request.referrer))
    response.set_cookie('user', '', expires=0)
    return response

@app.route('/productpage')
def front():
    headers = getForwardHeaders(request)
    user = request.cookies.get("user", "")
    bookdetails = getDetails(headers)
    bookreviews = getReviews(headers)
    return render_template('productpage.html', details=bookdetails, reviews=bookreviews, user=user)

def getReviews(headers):
    for i in range(2):
        try:
            res = requests.get(reviews['name']+"/"+reviews['endpoint'], headers=headers, timeout=3.0)
        except:
            res = None

        if res and res.status_code == 200:
            return res.text

    return """<h3>Sorry, product reviews are currently unavailable for this book.</h3>"""


def getDetails(headers):
    try:
        res = requests.get(details['name']+"/"+details['endpoint'], headers=headers, timeout=1.0)
    except:
        res = None

    if res and res.status_code == 200:
        return res.text
    else:
        return """<h3>Sorry, product details are currently unavailable for this book.</h3>"""


class Writer(object):

    def __init__(self, filename):
        self.file = open(filename,'w')

    def write(self, data):
        self.file.write(data)
        self.file.flush()


if __name__ == '__main__':
    if len(sys.argv) < 2:
        print "usage: %s port" % (sys.argv[0])
        sys.exit(-1)

    p = int(sys.argv[1])
    sys.stderr = Writer('stderr.log')
    sys.stdout = Writer('stdout.log')
    app.run(host='0.0.0.0', port=p, debug = True, threaded=True)

