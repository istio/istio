// Copyright 2017 Istio Authors.
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

package ui // import "istio.io/fortio/ui"

import (
	"bytes"
	// md5 is mandated, not our choice
	"crypto/md5" // nolint: gas
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"html/template"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"istio.io/fortio/fhttp"
	"istio.io/fortio/fnet"
	"istio.io/fortio/log"
	"istio.io/fortio/periodic"
	"istio.io/fortio/stats"
)

// TODO: move some of those in their own files/package (e.g data transfer TSV)
// and add unit tests.

var (
	// UI and Debug prefix/paths (read in ui handler).
	uiPath      string // absolute (base)
	logoPath    string // relative
	chartJSPath string // relative
	debugPath   string // mostly relative
	fetchPath   string // this one is absolute
	// Used to construct default URL to self.
	urlHostPort string
	// Start time of the UI Server (for uptime info).
	startTime time.Time
	// Directory where the static content and templates are to be loaded from.
	// This is replaced at link time to the packaged directory (e.g /usr/local/lib/fortio/)
	// but when fortio is installed with go get we use RunTime to find that directory.
	// (see Dockerfile for how to set it)
	resourcesDir     string
	extraBrowseLabel string // Extra label for report only
	// Directory where results are written to/read from
	dataDir        string
	mainTemplate   *template.Template
	browseTemplate *template.Template
	syncTemplate   *template.Template
	uiRunMapMutex  = &sync.Mutex{}
	id             int64
	runs           = make(map[int64]*periodic.RunnerOptions)
	// Base URL used for index - useful when running under an ingress with prefix
	baseURL string
)

const (
	fetchURI    = "fetch/"
	faviconPath = "/favicon.ico"
)

// Gets the resources directory from one of 3 sources:
func getResourcesDir(override string) string {
	if override != "" {
		log.Infof("Using resources directory from override: %s", override)
		return override
	}
	if resourcesDir != "" {
		log.LogVf("Using resources directory set at link time: %s", resourcesDir)
		return resourcesDir
	}
	_, filename, _, ok := runtime.Caller(0)
	log.LogVf("Guessing resources directory from runtime source location: %v - %s", ok, filename)
	if ok {
		return path.Dir(filename)
	}
	log.Errf("Unable to get source tree location. Failing to serve static contents.")
	return ""
}

// HTMLEscapeWriter is an io.Writer escaping the output for safe html inclusion.
type HTMLEscapeWriter struct {
	NextWriter io.Writer
	Flusher    http.Flusher
}

func (w *HTMLEscapeWriter) Write(p []byte) (int, error) {
	template.HTMLEscape(w.NextWriter, p)
	if w.Flusher != nil {
		w.Flusher.Flush()
	}
	return len(p), nil
}

// TODO: auto map from (Http)RunnerOptions to form generation and/or accept
// JSON serialized options as input.

// TODO: unit tests, allow additional data sets.

type mode int

// The main html has 3 principal modes:
const (
	// Default: renders the forms/menus
	menu mode = iota
	// Trigger a run
	run
	// Request abort
	stop
)

// Handler is the main UI handler creating the web forms and processing them.
func Handler(w http.ResponseWriter, r *http.Request) {
	LogRequest(r, "UI")
	mode := menu
	JSONOnly := false
	DoSave := (r.FormValue("save") == "on")
	url := r.FormValue("url")
	runid := int64(0)
	if r.FormValue("load") == "Start" {
		mode = run
		if r.FormValue("json") == "on" {
			JSONOnly = true
			log.Infof("Starting JSON only load request from %v for %s", r.RemoteAddr, url)
		} else {
			log.Infof("Starting load request from %v for %s", r.RemoteAddr, url)
		}
	} else {
		if r.FormValue("stop") == "Stop" {
			runid, _ = strconv.ParseInt(r.FormValue("runid"), 10, 64) // nolint: gas
			log.Critf("Stop request from %v for %d", r.RemoteAddr, runid)
			mode = stop
		}
	}
	// Those only exist/make sense on run mode but go variable declaration...
	labels := r.FormValue("labels")
	resolution, _ := strconv.ParseFloat(r.FormValue("r"), 64) // nolint: gas
	percList, _ := stats.ParsePercentiles(r.FormValue("p"))   // nolint: gas
	qps, _ := strconv.ParseFloat(r.FormValue("qps"), 64)      // nolint: gas
	durStr := r.FormValue("t")
	var dur time.Duration
	if durStr == "on" || ((len(r.Form["t"]) > 1) && r.Form["t"][1] == "on") {
		dur = -1
	} else {
		var err error
		dur, err = time.ParseDuration(durStr)
		if mode == run && err != nil {
			log.Errf("Error parsing duration '%s': %v", durStr, err)
		}
	}
	c, _ := strconv.Atoi(r.FormValue("c")) // nolint: gas
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Fatalf("expected http.ResponseWriter to be an http.Flusher")
	}
	out := io.Writer(os.Stderr)
	if !JSONOnly {
		out = io.Writer(&HTMLEscapeWriter{NextWriter: w, Flusher: flusher})
	}
	n, _ := strconv.ParseInt(r.FormValue("n"), 10, 64) // nolint: gas
	if strings.TrimSpace(url) == "" {
		url = "http://url.needed" // just because url validation doesn't like empty urls
	}
	opts := fhttp.NewHTTPOptions(url)
	ro := periodic.RunnerOptions{
		QPS:         qps,
		Duration:    dur,
		Out:         out,
		NumThreads:  c,
		Resolution:  resolution,
		Percentiles: percList,
		Labels:      labels,
		Exactly:     n,
	}
	if mode == run {
		ro.Normalize()
		uiRunMapMutex.Lock()
		id++ // start at 1 as 0 means interrupt all
		runid = id
		runs[runid] = &ro
		uiRunMapMutex.Unlock()
		log.Infof("New run id %d", runid)
	}
	if !JSONOnly {
		// Normal html mode
		if mainTemplate == nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Critf("Nil template")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		durSeconds := dur.Seconds()
		if n > 0 {
			if qps > 0 {
				durSeconds = float64(n) / qps
			} else {
				durSeconds = -1
			}
			log.Infof("Estimating fixed #call %d duration to %g seconds %g", n, durSeconds, qps)
		}
		err := mainTemplate.Execute(w, &struct {
			R                           *http.Request
			Headers                     http.Header
			Version                     string
			LogoPath                    string
			DebugPath                   string
			ChartJSPath                 string
			StartTime                   string
			TargetURL                   string
			Labels                      string
			RunID                       int64
			UpTime                      time.Duration
			TestExpectedDurationSeconds float64
			URLHostPort                 string
			DoStop                      bool
			DoLoad                      bool
		}{r, opts.GetHeaders(), periodic.Version, logoPath, debugPath, chartJSPath,
			startTime.Format(time.ANSIC), url, labels, runid,
			fhttp.RoundDuration(time.Since(startTime)), durSeconds, urlHostPort, mode == stop, mode == run})
		if err != nil {
			log.Critf("Template execution failed: %v", err)
		}
	}
	switch mode {
	case menu:
		// nothing more to do
	case stop:
		if runid <= 0 { // Stop all
			i := 0
			uiRunMapMutex.Lock()
			for _, v := range runs {
				v.Abort()
				i++
			}
			uiRunMapMutex.Unlock()
			log.Infof("Interrupted %d runs", i)
		} else { // Stop one
			uiRunMapMutex.Lock()
			v, found := runs[runid]
			if found {
				v.Abort()
			}
			uiRunMapMutex.Unlock()
		}
	case run:
		// mode == run case:
		firstHeader := true
		for _, header := range r.Form["H"] {
			if len(header) == 0 {
				continue
			}
			log.LogVf("adding header %v", header)
			if firstHeader {
				// If there is at least 1 non empty H passed, reset the header list
				opts.ResetHeaders()
				firstHeader = false
			}
			err := opts.AddAndValidateExtraHeader(header)
			if err != nil {
				log.Errf("Error adding custom headers: %v", err)
			}
		}
		onBehalfOf(opts, r)
		o := fhttp.HTTPRunnerOptions{
			RunnerOptions:      ro,
			HTTPOptions:        *opts,
			AllowInitialErrors: true,
		}
		if !JSONOnly {
			flusher.Flush()
		}
		res, err := fhttp.RunHTTPTest(&o)
		uiRunMapMutex.Lock()
		delete(runs, runid)
		uiRunMapMutex.Unlock()
		if err != nil {
			log.Errf("Init error %+v : %v", o, err)
			// nolint: errcheck,gas
			w.Write([]byte(fmt.Sprintf(
				"Aborting because %s\n</pre><script>document.getElementById('running').style.display = 'none';</script></body></html>\n",
				html.EscapeString(err.Error()))))
			return
		}
		json, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			log.Fatalf("Unable to json serialize result: %v", err)
		}
		savedAs := ""
		if DoSave {
			savedAs = SaveJSON(res.ID(), json)
		}
		if JSONOnly {
			w.Header().Set("Content-Type", "application/json")
			_, err = w.Write(json)
			if err != nil {
				log.Errf("Unable to write json output for %v: %v", r.RemoteAddr, err)
			}
			return
		}
		if savedAs != "" {
			// nolint: errcheck, gas
			w.Write([]byte(fmt.Sprintf("Saved result to <a href='%s'>%s</a>\n", savedAs, savedAs)))
		}
		// nolint: errcheck, gas
		w.Write([]byte(fmt.Sprintf("All done %d calls %.3f ms avg, %.1f qps\n</pre>\n<script>\n",
			res.DurationHistogram.Count,
			1000.*res.DurationHistogram.Avg,
			res.ActualQPS)))
		ResultToJsData(w, json)
		w.Write([]byte("</script></body></html>\n")) // nolint: gas
	}
}

// ResultToJsData converts a result object to chart data arrays and title
// and creates a chart from the result object
func ResultToJsData(w io.Writer, json []byte) {
	// nolint: errcheck, gas
	w.Write([]byte("var res = "))
	// nolint: errcheck, gas
	w.Write(json)
	// nolint: errcheck, gas
	w.Write([]byte("\nvar data = fortioResultToJsChartData(res)\nshowChart(data)\n"))
}

// SaveJSON save Json bytes to give file name (.json) in data-path dir.
func SaveJSON(name string, json []byte) string {
	if dataDir == "" {
		log.Infof("Not saving because data-path is unset")
		return ""
	}
	name += ".json"
	log.Infof("Saving %s in %s", name, dataDir)
	err := ioutil.WriteFile(path.Join(dataDir, name), json, 0644)
	if err != nil {
		log.Errf("Unable to save %s in %s: %v", name, dataDir, err)
		return ""
	}
	// Return the relative path from the /fortio/ UI
	return "data/" + name
}

// DataList returns the .json files/entries in data dir.
func DataList() (dataList []string) {
	files, err := ioutil.ReadDir(dataDir)
	if err != nil {
		log.Critf("Can list directory %s: %v", dataDir, err)
		return
	}
	// Newest files at the top:
	for i := len(files) - 1; i >= 0; i-- {
		name := files[i].Name()
		ext := ".json"
		if !strings.HasSuffix(name, ext) || files[i].IsDir() {
			log.LogVf("Skipping non %s file: %s", ext, name)
			continue
		}
		dataList = append(dataList, name[:len(name)-len(ext)])
	}
	log.LogVf("data list is %v (out of %d files in %s)", dataList, len(files), dataDir)
	return dataList
}

// BrowseHandler handles listing and rendering the JSON results.
func BrowseHandler(w http.ResponseWriter, r *http.Request) {
	LogRequest(r, "Browse")
	path := r.URL.Path
	if (path != uiPath) && (path != (uiPath + "browse")) {
		if strings.HasPrefix(path, "/fortio") {
			log.Infof("Redirecting /fortio in browse only path '%s'", path)
			http.Redirect(w, r, uiPath, http.StatusSeeOther)
		} else {
			log.Infof("Illegal browse path '%s'", path)
			w.WriteHeader(http.StatusNotFound)
		}
		return
	}
	url := r.FormValue("url")
	search := r.FormValue("s")
	doRender := (url != "")
	dataList := DataList()
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	err := browseTemplate.Execute(w, &struct {
		R           *http.Request
		Extra       string
		Version     string
		LogoPath    string
		ChartJSPath string
		URL         string
		Search      string
		DataList    []string
		URLHostPort string
		DoRender    bool
		DoSearch    bool
	}{r, extraBrowseLabel, periodic.Version, logoPath, chartJSPath,
		url, search, dataList, urlHostPort, doRender, (search != "")})
	if err != nil {
		log.Critf("Template execution failed: %v", err)
	}
}

// LogRequest logs the incoming request, including headers when loglevel is verbose
func LogRequest(r *http.Request, msg string) {
	log.Infof("%s: %v %v %v %v (%s)", msg, r.Method, r.URL, r.Proto, r.RemoteAddr,
		r.Header.Get("X-Forwarded-Proto"))
	if log.LogVerbose() {
		for name, headers := range r.Header {
			for _, h := range headers {
				log.LogVf("Header %v: %v\n", name, h)
			}
		}
	}
}

// LogAndAddCacheControl logs the request and wrapps an HTTP handler to add a Cache-Control header for static files.
func LogAndAddCacheControl(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		LogRequest(r, "Static")
		path := r.URL.Path
		if path == faviconPath {
			r.URL.Path = "/static/img" + faviconPath // fortio/version expected to be stripped already
			log.LogVf("Changed favicon internal path to %s", r.URL.Path)
		}
		w.Header().Set("Cache-Control", "max-age=365000000, immutable")
		h.ServeHTTP(w, r)
	})
}

func sendHTMLDataIndex(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.Write([]byte("<html><body><ul>\n")) // nolint: errcheck, gas
	for _, e := range DataList() {
		w.Write([]byte("<li><a href=\"")) // nolint: errcheck, gas
		w.Write([]byte(e))                // nolint: errcheck, gas
		w.Write([]byte(".json\">"))       // nolint: errcheck, gas
		w.Write([]byte(e))                // nolint: errcheck, gas
		w.Write([]byte("</a>\n"))         // nolint: errcheck, gas
	}
	w.Write([]byte("</ul></body></html>")) // nolint: errcheck, gas
}

type tsvCache struct {
	cachedDirTime time.Time
	cachedResult  []byte
}

var (
	gTSVCache      tsvCache
	gTSVCacheMutex = &sync.Mutex{}
)

// format for gcloud transfer
// https://cloud.google.com/storage/transfer/create-url-list
func sendTSVDataIndex(urlPrefix string, w http.ResponseWriter) {
	info, err := os.Stat(dataDir)
	if err != nil {
		log.Errf("Unable to stat %s: %v", dataDir, err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	gTSVCacheMutex.Lock() // Kind of a long time to hold a lock... hopefully the FS doesn't hang...
	useCache := (info.ModTime() == gTSVCache.cachedDirTime) && (len(gTSVCache.cachedResult) > 0)
	if !useCache {
		var b bytes.Buffer
		b.Write([]byte("TsvHttpData-1.0\n")) // nolint: errcheck, gas
		for _, e := range DataList() {
			fname := e + ".json"
			f, err := os.Open(path.Join(dataDir, fname))
			if err != nil {
				log.Errf("Open error for %s: %v", fname, err)
				continue
			}
			// This isn't a crypto hash, more like a checksum - and mandated by the
			// spec above, not our choice
			h := md5.New() // nolint: gas
			var sz int64
			if sz, err = io.Copy(h, f); err != nil {
				f.Close() // nolint: errcheck, gas
				log.Errf("Copy/read error for %s: %v", fname, err)
				continue
			}
			b.Write([]byte(urlPrefix))                                     // nolint: errcheck, gas
			b.Write([]byte(fname))                                         // nolint: errcheck, gas
			b.Write([]byte("\t"))                                          // nolint: errcheck, gas
			b.Write([]byte(strconv.FormatInt(sz, 10)))                     // nolint: errcheck, gas
			b.Write([]byte("\t"))                                          // nolint: errcheck, gas
			b.Write([]byte(base64.StdEncoding.EncodeToString(h.Sum(nil)))) // nolint: errcheck, gas
			b.Write([]byte("\n"))                                          // nolint: errcheck, gas
		}
		gTSVCache.cachedDirTime = info.ModTime()
		gTSVCache.cachedResult = b.Bytes()
	}
	result := gTSVCache.cachedResult
	lastModified := gTSVCache.cachedDirTime.Format(http.TimeFormat)
	gTSVCacheMutex.Unlock()
	log.Infof("Used cached %v to serve %d bytes TSV", useCache, len(result))
	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	// Cloud transfer requires ETag
	w.Header().Set("ETag", fmt.Sprintf("\"%s\"", lastModified))
	w.Header().Set("Last-Modified", lastModified)
	w.Write(result) // nolint: errcheck, gas
}

// LogAndFilterDataRequest logs the data request.
func LogAndFilterDataRequest(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		LogRequest(r, "Data")
		path := r.URL.Path
		if strings.HasSuffix(path, "/") || strings.HasSuffix(path, "/index.html") {
			sendHTMLDataIndex(w)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		ext := "/index.tsv"
		if strings.HasSuffix(path, ext) {
			// Ingress effect:
			urlPrefix := baseURL
			if len(urlPrefix) == 0 {
				// The Host header includes original host/port, only missing is the proto:
				proto := r.Header.Get("X-Forwarded-Proto")
				if len(proto) == 0 {
					proto = "http"
				}
				urlPrefix = proto + "://" + r.Host + path[:len(path)-len(ext)+1]
			} else {
				urlPrefix += uiPath + "data/" // base has been cleaned of trailing / in fortio_main
			}
			log.Infof("Prefix is '%s'", urlPrefix)
			sendTSVDataIndex(urlPrefix, w)
			return
		}
		if !strings.HasSuffix(path, ".json") {
			log.Warnf("Filtering request for non .json '%s'", path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// FetcherHandler is the handler for the fetcher/proxy.
func FetcherHandler(w http.ResponseWriter, r *http.Request) {
	LogRequest(r, "Fetch")
	hj, ok := w.(http.Hijacker)
	if !ok {
		log.Critf("hijacking not supported")
		return
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		log.Errf("hijacking error %v", err)
		return
	}
	// Don't forget to close the connection:
	defer conn.Close() // nolint: errcheck
	url := r.URL.String()[len(fetchPath):]
	opts := fhttp.NewHTTPOptions("http://" + url)
	opts.HTTPReqTimeOut = 5 * time.Minute
	onBehalfOf(opts, r)
	client := fhttp.NewClient(opts)
	if client == nil {
		return // error logged already
	}
	_, data, _ := client.Fetch()
	_, err = conn.Write(data)
	if err != nil {
		log.Errf("Error writing fetched data to %v: %v", r.RemoteAddr, err)
	}
}

func onBehalfOf(o *fhttp.HTTPOptions, r *http.Request) {
	_ = o.AddAndValidateExtraHeader("X-On-Behalf-Of: " + r.RemoteAddr) // nolint: gas
}

// TODO: move tsv/xml sync handling to their own file (and possibly package)

// http.ResponseWriter + Flusher emulator - if we refactor the code this should
// not be needed. on the other hand it's useful and could be reused.
type outHTTPWriter struct {
	CodePtr *int // Needed because that interface is somehow pass by value
	Out     io.Writer
	header  http.Header
}

func (o outHTTPWriter) Header() http.Header {
	return o.header
}

func (o outHTTPWriter) Write(b []byte) (int, error) {
	return o.Out.Write(b)
}

func (o outHTTPWriter) WriteHeader(code int) {
	*o.CodePtr = code
	o.Out.Write([]byte(fmt.Sprintf("\n*** result code: %d\n", code))) // nolint: gas, errcheck
}

func (o outHTTPWriter) Flush() {
	// nothing
}

// Sync is the non http equivalent of fortio/sync?url=u.
func Sync(out io.Writer, u string, datadir string) bool {
	dataDir = datadir
	v := url.Values{}
	v.Set("url", u)
	req, _ := http.NewRequest("GET", "/sync-function?"+v.Encode(), nil) // nolint: gas
	code := http.StatusOK                                               // default
	w := outHTTPWriter{Out: out, CodePtr: &code}
	SyncHandler(w, req)
	return (code == http.StatusOK)
}

// SyncHandler handles syncing/downloading from tsv url.
func SyncHandler(w http.ResponseWriter, r *http.Request) {
	LogRequest(r, "Sync")
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Fatalf("expected http.ResponseWriter to be an http.Flusher")
	}
	uStr := strings.TrimSpace(r.FormValue("url"))
	if syncTemplate != nil {
		err := syncTemplate.Execute(w, &struct {
			Version  string
			LogoPath string
			URL      string
		}{periodic.Version, logoPath, uStr})
		if err != nil {
			log.Critf("Sync template execution failed: %v", err)
		}
	}
	w.Write([]byte("Fetch of index/bucket url ... ")) // nolint: gas, errcheck
	flusher.Flush()
	o := fhttp.NewHTTPOptions(uStr)
	onBehalfOf(o, r)
	// If we had hundreds of thousands of entry we should stream, parallelize (connection pool)
	// and not do multiple passes over the same data, but for small tsv this is fine.
	// use std client to change the url and handle https:
	client := fhttp.NewStdClient(o)
	if client == nil {
		w.Write([]byte("invalid url!<script>setPB(1,1)</script></body></html>\n")) // nolint: gas, errcheck
		w.WriteHeader(422 /*Unprocessable Entity*/)
		return
	}
	code, data, _ := client.Fetch()
	if code != http.StatusOK {
		w.Write([]byte(fmt.Sprintf("http error, code %d<script>setPB(1,1)</script></body></html>\n", code))) // nolint: gas, errcheck
		w.WriteHeader(424 /*Failed Dependency*/)
		return
	}
	sdata := strings.TrimSpace(string(data))
	if strings.HasPrefix(sdata, "TsvHttpData-1.0") {
		processTSV(w, client, sdata)
	} else {
		if !processXML(w, client, data, uStr, 0) {
			return
		}
	}
	w.Write([]byte("</table>"))           // nolint: gas, errcheck
	w.Write([]byte("\n</body></html>\n")) // nolint: gas, errcheck
}

func processTSV(w http.ResponseWriter, client *fhttp.Client, sdata string) {
	flusher := w.(http.Flusher)
	lines := strings.Split(sdata, "\n")
	n := len(lines)
	// nolint: gas, errcheck
	w.Write([]byte(fmt.Sprintf("success tsv fetch! Now fetching %d referenced URLs:<script>setPB(1,%d)</script>\n",
		n-1, n)))
	w.Write([]byte("<table>")) // nolint: gas, errcheck
	flusher.Flush()
	for i, l := range lines[1:] {
		parts := strings.Split(l, "\t")
		u := parts[0]
		w.Write([]byte("<tr><td>"))                   // nolint: gas, errcheck
		w.Write([]byte(template.HTMLEscapeString(u))) // nolint: gas, errcheck
		ur, err := url.Parse(u)
		if err != nil {
			w.Write([]byte("<td>skipped (not a valid url)")) // nolint: gas, errcheck
		} else {
			uPath := ur.Path
			pathParts := strings.Split(uPath, "/")
			name := pathParts[len(pathParts)-1]
			downloadOne(w, client, name, u)
		}
		w.Write([]byte(fmt.Sprintf("</tr><script>setPB(%d)</script>\n", i+2))) // nolint: gas, errcheck
		flusher.Flush()
	}
}

// ListBucketResult is the minimum we need out of s3 xml results.
// https://docs.aws.amazon.com/AmazonS3/latest/API/RESTBucketGET.html
// e.g. https://storage.googleapis.com/fortio-data?max-keys=2&prefix=fortio.istio.io/
type ListBucketResult struct {
	NextMarker string   `xml:"NextMarker"`
	Names      []string `xml:"Contents>Key"`
}

// @returns true if started a table successfully - false is error
func processXML(w http.ResponseWriter, client *fhttp.Client, data []byte, baseURL string, level int) bool {
	// We already know this parses as we just fetched it:
	bu, _ := url.Parse(baseURL) // nolint: gas, errcheck
	flusher := w.(http.Flusher)
	l := ListBucketResult{}
	err := xml.Unmarshal(data, &l)
	if err != nil {
		log.Errf("xml unmarshal error %v", err)
		// don't show the error / would need html escape to avoid CSS attacks
		w.Write([]byte("xml parsing error, check logs<script>setPB(1,1)</script></body></html>\n")) // nolint: gas, errcheck
		w.WriteHeader(http.StatusInternalServerError)
		return false
	}
	n := len(l.Names)
	log.Infof("Parsed %+v", l)
	// nolint: gas, errcheck
	w.Write([]byte(fmt.Sprintf("success xml fetch #%d! Now fetching %d referenced objects:<script>setPB(1,%d)</script>\n",
		level+1, n, n+1)))
	if level == 0 {
		w.Write([]byte("<table>")) // nolint: gas, errcheck
	}
	for i, el := range l.Names {
		w.Write([]byte("<tr><td>"))                    // nolint: gas, errcheck
		w.Write([]byte(template.HTMLEscapeString(el))) // nolint: gas, errcheck
		pathParts := strings.Split(el, "/")
		name := pathParts[len(pathParts)-1]
		newURL := *bu // copy
		newURL.Path = newURL.Path + "/" + el
		fullURL := newURL.String()
		downloadOne(w, client, name, fullURL)
		w.Write([]byte(fmt.Sprintf("</tr><script>setPB(%d)</script>\n", i+2))) // nolint: gas, errcheck
		flusher.Flush()
	}
	flusher.Flush()
	// Is there more data ? (NextMarker present)
	if len(l.NextMarker) == 0 {
		return true
	}
	if level > 100 {
		log.Errf("Too many chunks, stopping after 100")
		w.WriteHeader(509 /* Bandwidth Limit Exceeded */)
		return true
	}
	q := bu.Query()
	if q.Get("marker") == l.NextMarker {
		log.Errf("Loop with same marker %+v", bu)
		w.WriteHeader(508 /* Loop Detected */)
		return true
	}
	q.Set("marker", l.NextMarker)
	bu.RawQuery = q.Encode()
	newBaseURL := bu.String()
	// url already validated
	w.Write([]byte("<tr><td>"))                            // nolint: gas, errcheck
	w.Write([]byte(template.HTMLEscapeString(newBaseURL))) // nolint: gas, errcheck
	w.Write([]byte("<td>"))                                // nolint: gas, errcheck
	_ = client.ChangeURL(newBaseURL)                       // nolint: gas
	ncode, ndata, _ := client.Fetch()
	if ncode != http.StatusOK {
		log.Errf("Can't fetch continuation with marker %+v", bu)
		// nolint: gas, errcheck
		w.Write([]byte(fmt.Sprintf("http error, code %d<script>setPB(1,1)</script></table></body></html>\n", ncode)))
		w.WriteHeader(424 /*Failed Dependency*/)
		return false
	}
	return processXML(w, client, ndata, newBaseURL, level+1) // recurse
}

func downloadOne(w http.ResponseWriter, client *fhttp.Client, name string, u string) {
	log.Infof("downloadOne(%s,%s)", name, u)
	if !strings.HasSuffix(name, ".json") {
		w.Write([]byte("<td>skipped (not json)")) // nolint: gas, errcheck
		return
	}
	localPath := path.Join(dataDir, name)
	_, err := os.Stat(localPath)
	if err == nil {
		w.Write([]byte("<td>skipped (already exists)")) // nolint: gas, errcheck
		return
	}
	// note that if data dir doesn't exist this will trigger too - TODO: check datadir earlier
	if !os.IsNotExist(err) {
		log.Warnf("check %s : %v", localPath, err)
		// don't return the details of the error to not leak local data dir etc
		w.Write([]byte("<td>skipped (access error)")) // nolint: gas, errcheck
		return
	}
	// url already validated
	_ = client.ChangeURL(u) // nolint: gas
	code1, data1, _ := client.Fetch()
	if code1 != http.StatusOK {
		w.Write([]byte(fmt.Sprintf("<td>Http error, code %d", code1))) // nolint: gas, errcheck
		w.WriteHeader(424 /*Failed Dependency*/)
		return
	}
	err = ioutil.WriteFile(localPath, data1, 0644)
	if err != nil {
		log.Errf("Unable to save %s: %v", localPath, err)
		w.Write([]byte("<td>skipped (write error)")) // nolint: gas, errcheck
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// finally ! success !
	log.Infof("Success fetching %s - saved at %s", u, localPath)
	// checkmark
	w.Write([]byte("<td class='checkmark'>✓")) // nolint: gas, errcheck
}

// Serve starts the fhttp.Serve() plus the UI server on the given port
// and paths (empty disables the feature). uiPath should end with /
// (be a 'directory' path)
func Serve(baseurl, port, debugpath, uipath, staticRsrcDir string, datadir string) {
	baseURL = baseurl
	startTime = time.Now()
	hostPort := setHostAndPort(fnet.NormalizePort(port))
	if uipath == "" {
		fhttp.Serve(hostPort, debugpath) // doesn't return until exit
		return
	}
	uiPath = uipath
	dataDir = datadir
	if uiPath[len(uiPath)-1] != '/' {
		log.Warnf("Adding missing trailing / to UI path '%s'", uiPath)
		uiPath += "/"
	}
	debugPath = ".." + debugpath // TODO: calculate actual path if not same number of directories
	http.HandleFunc(uiPath, Handler)
	uiMsg := fmt.Sprintf("UI starting - visit:\nhttp://%s%s", urlHostPort, uiPath)
	if !strings.Contains(port, ":") {
		uiMsg += "   (or any host/ip reachable on this server)"
	}
	fmt.Printf(uiMsg + "\n")
	fetchPath = uiPath + fetchURI
	http.HandleFunc(fetchPath, FetcherHandler)
	fhttp.CheckConnectionClosedHeader = true // needed for proxy to avoid errors

	logoPath = periodic.Version + "/static/img/logo.svg"
	chartJSPath = periodic.Version + "/static/js/Chart.min.js"

	// Serve static contents in the ui/static dir. If not otherwise specified
	// by the function parameter staticPath, we use getResourcesDir which uses the
	// link time value or the directory relative to this file to find the static
	// contents, so no matter where or how the go binary is generated, the static
	// dir should be found.
	staticRsrcDir = getResourcesDir(staticRsrcDir)
	if staticRsrcDir != "" {
		fs := http.FileServer(http.Dir(staticRsrcDir))
		prefix := uiPath + periodic.Version
		http.Handle(prefix+"/static/", LogAndAddCacheControl(http.StripPrefix(prefix, fs)))
		http.Handle(faviconPath, LogAndAddCacheControl(fs))
		var err error
		mainTemplate, err = template.ParseFiles(path.Join(staticRsrcDir, "templates/main.html"))
		if err != nil {
			log.Critf("Unable to parse main template: %v", err)
		}
		browseTemplate, err = template.ParseFiles(path.Join(staticRsrcDir, "templates/browse.html"))
		if err != nil {
			log.Critf("Unable to parse browse template: %v", err)
		} else {
			http.HandleFunc(uiPath+"browse", BrowseHandler)
		}
		syncTemplate, err = template.ParseFiles(path.Join(staticRsrcDir, "templates/sync.html"))
		if err != nil {
			log.Critf("Unable to parse sync template: %v", err)
		} else {
			http.HandleFunc(uiPath+"sync", SyncHandler)
		}
	}
	if dataDir != "" {
		fs := http.FileServer(http.Dir(dataDir))
		http.Handle(uiPath+"data/", LogAndFilterDataRequest(http.StripPrefix(uiPath+"data", fs)))
	}
	fhttp.Serve(hostPort, debugpath)
}

// Report starts the browsing only UI server on the given port.
// Similar to Serve with only the read only part.
func Report(baseurl, port, staticRsrcDir string, datadir string) {
	baseURL = baseurl
	extraBrowseLabel = ", report only limited UI"
	hostPort := setHostAndPort(fnet.NormalizePort(port))
	uiMsg := fmt.Sprintf("Browse only UI starting - visit:\nhttp://%s/", urlHostPort)
	if !strings.Contains(port, ":") {
		uiMsg += "   (or any host/ip reachable on this server)"
	}
	fmt.Printf(uiMsg + "\n")
	uiPath = "/"
	dataDir = datadir
	logoPath = periodic.Version + "/static/img/logo.svg"
	chartJSPath = periodic.Version + "/static/js/Chart.min.js"
	staticRsrcDir = getResourcesDir(staticRsrcDir)
	fs := http.FileServer(http.Dir(staticRsrcDir))
	prefix := uiPath + periodic.Version
	http.Handle(prefix+"/static/", LogAndAddCacheControl(http.StripPrefix(prefix, fs)))
	http.Handle(faviconPath, LogAndAddCacheControl(fs))
	var err error
	browseTemplate, err = template.ParseFiles(path.Join(staticRsrcDir, "templates/browse.html"))
	if err != nil {
		log.Critf("Unable to parse browse template: %v", err)
	} else {
		http.HandleFunc(uiPath, BrowseHandler)
	}
	fsd := http.FileServer(http.Dir(dataDir))
	http.Handle(uiPath+"data/", LogAndFilterDataRequest(http.StripPrefix(uiPath+"data", fsd)))
	if err := http.ListenAndServe(hostPort, nil); err != nil {
		log.Critf("Error starting report server: %v", err)
	}
}

// -- Redirection to https feature --

// RedirectToHTTPSHandler handler sends a redirect to same URL with https.
func RedirectToHTTPSHandler(w http.ResponseWriter, r *http.Request) {
	dest := "https://" + r.Host + r.URL.String()
	LogRequest(r, "Redirecting to "+dest)
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// RedirectToHTTPS Sets up a redirector to https on the given port.
// (Do not create a loop, make sure this is addressed from an ingress)
func RedirectToHTTPS(port int) {
	m := http.NewServeMux()
	m.HandleFunc("/", RedirectToHTTPSHandler)
	s := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: m,
	}
	fmt.Printf("Https redirector running on %v\n", s.Addr)
	if err := s.ListenAndServe(); err != nil {
		log.Critf("Error starting report server: %v", err)
	}
	fmt.Printf("Not reached, https redirector exiting - was on %v\n", s.Addr)
}

// setHostAndPort takes hostport in the form of hostname:port, ip:port or :port,
// sets the urlHostPort variable and returns hostport unmodified.
func setHostAndPort(hostport string) string {
	if strings.HasPrefix(hostport, ":") {
		urlHostPort = "localhost" + hostport
		return hostport
	}
	urlHostPort = hostport
	return hostport
}
