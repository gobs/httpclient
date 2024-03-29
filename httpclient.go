package httpclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gobs/pretty"
	"github.com/gobs/simplejson"
	//"net"
	//"github.com/jbenet/go-net-reuse"
)

const (
	DefaultTimeout  = 30 * time.Second
	DefaultMaxConns = 50
)

var (
	// we use our own default client, so we can change the TLS configuration
	DefaultClient                      = &http.Client{Timeout: DefaultTimeout}
	DefaultTransport http.RoundTripper = http.DefaultTransport.(*http.Transport).Clone()

	NoRedirect       = errors.New("No redirect")
	TooManyRedirects = errors.New("stopped after 10 redirects")
	NotModified      = errors.New("Not modified")
)

func init() {
	// some better defaults for the transport
	if tr, ok := DefaultTransport.(*http.Transport); ok {
		tr.MaxIdleConns = DefaultMaxConns
		tr.MaxConnsPerHost = DefaultMaxConns
		tr.MaxIdleConnsPerHost = DefaultMaxConns
	}
}

// Disable HTTP/2 client support.
// This is useful for doing stress tests when you want to create a lot of concurrent HTTP/1.1 connection
// (the HTTP/2 client would try to multiplex the requests on a single connection).
func DisableHttp2() {
	if err := os.Setenv("GODEBUG", "http2client=0"); err != nil {
		log.Fatal(err)
	}

	if tr, ok := DefaultTransport.(*http.Transport); ok {
		tr.ForceAttemptHTTP2 = false
	}
}

// Allow connections via HTTPS even if something is wrong with the certificate
// (self-signed or expired)
func AllowInsecure(insecure bool) {
	if tr, ok := DefaultTransport.(*http.Transport); ok {
		if insecure {
			tr := tr.Clone()
			tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

			DefaultClient.Transport = tr
		} else {
			DefaultClient.Transport = DefaultTransport
		}
	}
}

// Set connection timeout
func SetTimeout(t time.Duration) {
	DefaultClient.Timeout = t
}

// HTTP error
type HttpError struct {
	Code       int
	Message    string
	RetryAfter int
	Body       []byte
	Header     http.Header
}

func (e HttpError) Error() string {
	if len(e.Body) > 0 {
		return fmt.Sprintf("%v %s", e.Message, e.Body)
	} else {
		return e.Message
	}
}

func (e HttpError) String() string {
	if len(e.Body) > 0 {
		return fmt.Sprintf("ERROR: %v %v %s", e.Code, e.Message, e.Body)
	} else {
		return fmt.Sprintf("ERROR: %v %v", e.Code, e.Message)
	}
}

// CloseResponse makes sure we close the response body
func CloseResponse(r *http.Response) {
	if r != nil && r.Body != nil {
		io.Copy(ioutil.Discard, r.Body)
		r.Body.Close()
	}
}

// A wrapper for http.Response
type HttpResponse struct {
	http.Response
}

// ContentType returns the response content type
func (r *HttpResponse) ContentType() string {
	content_type := r.Header.Get("Content-Type")
	if len(content_type) == 0 {
		return content_type
	}

	return strings.TrimSpace(strings.Split(content_type, ";")[0])
}

// ContentDisposition returns the content disposition type, field name and filename values
func (r *HttpResponse) ContentDisposition() (ctype, name, filename string) {
	content_disp := r.Header.Get("Content-Disposition")
	if len(content_disp) == 0 {
		return
	}

	parts := strings.Split(content_disp, ";")
	ctype = parts[0]

	for _, p := range parts[1:] {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "name=") {
			name = strings.Trim(p[5:], `"`)
		} else if strings.HasPrefix(p, "filename=") {
			filename = strings.Trim(p[9:], `"`)
			//} else if strings.Hasprefix(p, "filename=") {
			//    filename = strings.Trim(p[10:], `"`) // need decoding
		}
	}

	return
}

// Close makes sure that all data from the body is read
// before closing the reader.
//
// If that is not the desider behaviour, just call HttpResponse.Body.Close()
func (r *HttpResponse) Close() {
	if r != nil {
		CloseResponse(&r.Response)
	}
}

// ResponseError checks the StatusCode and return an error if needed.
// The error is of type HttpError
func (r *HttpResponse) ResponseError() error {
	class := r.StatusCode / 100
	if class != 2 && class != 3 {
		rt := 0

		if h := r.Header.Get("Retry-After"); h != "" {
			rt, _ = strconv.Atoi(h)
		}

		var body [256]byte
		var blen int

		if r.Body != nil {
			blen, _ = r.Body.Read(body[:])
		}

		return HttpError{Code: r.StatusCode,
			Message:    "HTTP " + r.Status,
			RetryAfter: rt,
			Header:     r.Header,
			Body:       body[:blen],
		}
	}

	if r.StatusCode == http.StatusNotModified {
		return NotModified
	}

	return nil
}

// CheckStatus returns err if not null or an HTTP status if the response was not "succesfull"
//
// usage:
//
//	resp, err := httpclient.CheckStatus(httpclient.SendRequest(params...))
func CheckStatus(r *HttpResponse, err error) (*HttpResponse, error) {
	if err != nil {
		return r, err
	}

	return r, r.ResponseError()
}

// Check if the input value is a "primitive" that can be safely stringified
func canStringify(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.String:
		return true
	default:
		return false
	}
}

// ParamValues fills the input url.Values according to params
func ParamValues(params map[string]interface{}, q url.Values) url.Values {
	if q == nil {
		q = url.Values{}
	}

	for k, v := range params {
		val := reflect.ValueOf(v)

		switch val.Kind() {
		case reflect.Slice:
			if val.IsNil() { // TODO: add an option to ignore empty values
				q.Set(k, "")
				continue
			}
			fallthrough

		case reflect.Array:
			for i := 0; i < val.Len(); i++ {
				av := val.Index(i)

				if canStringify(av) {
					q.Add(k, fmt.Sprintf("%v", av))
				}
			}

		default:
			if canStringify(val) {
				q.Set(k, fmt.Sprintf("%v", v))
			} else {
				log.Fatal("Invalid type ", val)
			}
		}
	}

	return q
}

// Given a base URL and a bag of parameteters returns the URL with the encoded parameters
func URLWithPathParams(base string, path string, params map[string]interface{}) (u *url.URL) {

	u, err := url.Parse(base)
	if err != nil {
		log.Fatal(err)
	}

	if len(path) > 0 {
		u, err = u.Parse(path)
		if err != nil {
			log.Fatal(err)
		}
	}

	q := ParamValues(params, u.Query())
	u.RawQuery = q.Encode()
	return u
}

func URLWithParams(base string, params map[string]interface{}) (u *url.URL) {
	return URLWithPathParams(base, "", params)
}

// http.Get with params
func Get(urlStr string, params map[string]interface{}) (*HttpResponse, error) {
	resp, err := DefaultClient.Get(URLWithParams(urlStr, params).String())
	if err == nil {
		return &HttpResponse{*resp}, nil
	} else {
		CloseResponse(resp)
		return nil, err
	}
}

// http.Post with params
func Post(urlStr string, params map[string]interface{}) (*HttpResponse, error) {
	resp, err := DefaultClient.PostForm(urlStr, URLWithParams(urlStr, params).Query())
	if err == nil {
		return &HttpResponse{*resp}, nil
	} else {
		CloseResponse(resp)
		return nil, err
	}
}

// Read the body
func (resp *HttpResponse) Content() []byte {
	if resp == nil {
		return nil
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err, " - read ", len(body), " bytes")
	}
	resp.Body.Close()
	return body
}

// Try to parse the response body as JSON
func (resp *HttpResponse) Json() (json *simplejson.Json) {
	json, _ = simplejson.LoadBytes(resp.Content())
	return
}

// JsonDecode decodes the response body as JSON into specified structure
func (resp *HttpResponse) JsonDecode(out interface{}, strict bool) error {
	dec := json.NewDecoder(resp.Body)
	if strict {
		dec.DisallowUnknownFields()
	}
	defer resp.Body.Close()
	return dec.Decode(out)
}

// XmlDecode decodes the response body as XML into specified structure
func (resp *HttpResponse) XmlDecode(out interface{}, strict bool) error {
	dec := xml.NewDecoder(resp.Body)
	dec.Strict = strict
	defer resp.Body.Close()
	return dec.Decode(out)
}

////////////////////////////////////////////////////////////////////////

// http.Client with some defaults and stuff
type HttpClient struct {
	// the http.Client
	client *http.Client

	// the base URL for this client
	BaseURL *url.URL

	// overrides Host header
	Host string

	// the client UserAgent string
	UserAgent string

	// Common headers to be passed on each request
	Headers map[string]string

	// Cookies to be passed on each request
	Cookies []*http.Cookie

	// if FollowRedirects is false, a 30x response will be returned as is
	FollowRedirects bool

	// if HeadRedirects is true, the client will follow the redirect also for HEAD requests
	HeadRedirects bool

	// if Verbose, log request and response info
	Verbose bool

	// if Close, all requests will set Connection: close
	// (no keep-alive)
	Close bool
}

func cloneDefaultTransport() http.RoundTripper {
	if cl, ok := DefaultTransport.(interface{ Clone() http.RoundTripper }); ok {
		return cl.Clone()
	}

	return DefaultTransport
}

// Create a new HttpClient
func NewHttpClient(base string) (httpClient *HttpClient) {
	httpClient = new(HttpClient)
	httpClient.client = &http.Client{
		CheckRedirect: httpClient.checkRedirect,
		Transport:     cloneDefaultTransport(),
		Timeout:       DefaultTimeout,
	}
	httpClient.Headers = make(map[string]string)
	httpClient.FollowRedirects = true

	if err := httpClient.SetBase(base); err != nil {
		log.Fatal(err)
	}

	return
}

// Clone an HttpClient (re-use the same http.Client but duplicate the headers)
func (self *HttpClient) Clone() *HttpClient {
	clone := *self
	clone.Headers = make(map[string]string, len(self.Headers))
	for k, v := range self.Headers {
		clone.Headers[k] = v
	}

	return &clone
}

// Set Base
func (self *HttpClient) SetBase(base string) error {
	u, err := url.Parse(base)
	if err != nil {
		return err
	}

	self.BaseURL = u
	return nil
}

// Set Transport
func (self *HttpClient) SetTransport(tr http.RoundTripper) {
	self.client.Transport = tr
}

// Get current Transport
func (self *HttpClient) GetTransport() http.RoundTripper {
	return self.client.Transport
}

// Set CookieJar
func (self *HttpClient) SetCookieJar(jar http.CookieJar) {
	self.client.Jar = jar
}

// Get current CookieJar
func (self *HttpClient) GetCookieJar() http.CookieJar {
	return self.client.Jar
}

// Allow connections via HTTPS even if something is wrong with the certificate
// (self-signed or expired)
func (self *HttpClient) AllowInsecure(insecure bool) {
	var config *tls.Config
	if insecure {
		config = &tls.Config{InsecureSkipVerify: true}
	}

	if tr, ok := self.client.Transport.(*http.Transport); ok {
		tr.TLSClientConfig = config
	} else if tr, ok := self.client.Transport.(*LoggingTransport); ok {
		tr.t.(*http.Transport).TLSClientConfig = config
	}
}

// Set connection timeout
func (self *HttpClient) SetTimeout(t time.Duration) {
	self.client.Timeout = t

	if tr, ok := self.client.Transport.(*http.Transport); ok {
		tr.TLSHandshakeTimeout = t
	} else if tr, ok := self.client.Transport.(*LoggingTransport); ok {
		tr.t.(*http.Transport).TLSHandshakeTimeout = t
	}
}

// Get connection timeout
func (self *HttpClient) GetTimeout() time.Duration {
	return self.client.Timeout
}

// Enable request logging for this client
func (self *HttpClient) StartLogging(requestBody, responseBody, timing bool) {
	if ltr, ok := self.client.Transport.(*LoggingTransport); ok {
		ltr.requestBody = requestBody
		ltr.responseBody = responseBody
		ltr.timing = timing
	} else {
		self.SetTransport(LoggedTransport(self.client.Transport, true, true, true))
	}
}

// Disable request logging for this client
func (self *HttpClient) StopLogging() {
	if ltr, ok := self.client.Transport.(*LoggingTransport); ok {
		self.SetTransport(ltr.t)
	}
}

// add default headers plus extra headers
func (self *HttpClient) addHeaders(req *http.Request, headers map[string]string) {

	if len(self.UserAgent) > 0 {
		req.Header.Set("User-Agent", self.UserAgent)
	}

	for k, v := range self.Headers {
		if _, add := headers[k]; !add {
			req.Header.Set(k, v)
		}
	}

	for _, c := range self.Cookies {
		req.AddCookie(c)
	}

	for k, v := range headers {
		if strings.ToLower(k) == "content-length" {
			if len, err := strconv.Atoi(v); err == nil && req.ContentLength <= 0 {
				req.ContentLength = int64(len)
			}
		} else if v != "" {
			req.Header.Set(k, v)
		} else {
			req.Header.Del(k)
		}
	}
}

// the callback for CheckRedirect, used to pass along the headers in case of redirection
func (self *HttpClient) checkRedirect(req *http.Request, via []*http.Request) error {
	if !self.FollowRedirects {
		// don't follow redirects if explicitly disabled
		return NoRedirect
	}

	if req.Method == "HEAD" && !self.HeadRedirects {
		// don't follow redirects on a HEAD request
		return NoRedirect
	}

	DebugLog(self.Verbose).Println("REDIRECT:", len(via), req.URL)
	if len(req.Cookies()) > 0 {
		DebugLog(self.Verbose).Println("COOKIES:", req.Cookies())
	}

	if len(via) >= 10 {
		return TooManyRedirects
	}

	if len(via) > 0 {
		last := via[len(via)-1]
		if len(last.Cookies()) > 0 {
			DebugLog(self.Verbose).Println("LAST COOKIES:", last.Cookies())
		}
	}

	// TODO: check for same host before adding headers
	self.addHeaders(req, nil)
	return nil
}

// Create a request object given the method, path, body and extra headers
func (self *HttpClient) Request(method string, urlpath string, body io.Reader, headers map[string]string) (req *http.Request) {
	if self.BaseURL != nil {
		if u, err := self.BaseURL.Parse(urlpath); err != nil {
			log.Fatal(err)
		} else {
			urlpath = u.String()
		}
	}

	req, err := http.NewRequest(strings.ToUpper(method), urlpath, body)
	if err != nil {
		log.Fatal(err)
	}

	req.Close = self.Close
	req.Host = self.Host

	self.addHeaders(req, headers)

	return
}

////////////////////////////////////////////////////////////////////////////////////
//
// New style requests, with functional options

type RequestOption func(req *http.Request) (*http.Request, error)

// Set the request method
func Method(m string) RequestOption {
	return func(req *http.Request) (*http.Request, error) {
		req.Method = strings.ToUpper(m)
		return req, nil
	}
}

var (
	HEAD   = Method("HEAD")
	GET    = Method("GET")
	POST   = Method("POST")
	PUT    = Method("PUT")
	DELETE = Method("DELETE")
)

// set the request URL
func URL(u *url.URL) RequestOption {
	return func(req *http.Request) (*http.Request, error) {
		req.URL = u
		return req, nil
	}
}

// set the request URL (passed as string)
func URLString(ustring string) RequestOption {
	return func(req *http.Request) (*http.Request, error) {
		u, err := url.Parse(ustring)
		if err != nil {
			return nil, err
		}

		req.URL = u
		return req, nil
	}
}

// set the request path
func Path(path string) RequestOption {
	return func(req *http.Request) (*http.Request, error) {
		if req.URL != nil {
			u, err := req.URL.Parse(path)
			if err != nil {
				return nil, err
			}

			req.URL = u
			return req, nil
		}

		u, err := url.Parse(path)
		if err != nil {
			return nil, err
		}

		req.URL = u
		return req, nil
	}
}

func (c *HttpClient) Path(path string) RequestOption {
	return func(req *http.Request) (*http.Request, error) {
		var u *url.URL
		var err error

		if c.BaseURL == nil {
			u, err = url.Parse(path)
		} else {
			u, err = c.BaseURL.Parse(path)
		}

		if err != nil {
			return nil, err
		}

		req.URL = u
		return req, nil
	}
}

// set the request URL parameters
func Params(params map[string]interface{}) RequestOption {
	return func(req *http.Request) (*http.Request, error) {
		u := req.URL.String()
		req.URL = URLWithParams(u, params)
		return req, nil
	}
}

// set the request URL parameters
func StringParams(params map[string]string) RequestOption {
	return func(req *http.Request) (*http.Request, error) {
		q := req.URL.Query()
		for k, v := range params {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
		return req, nil
	}
}

// set the request body as an io.Reader
func Body(r io.Reader) RequestOption {
	return func(req *http.Request) (*http.Request, error) {
		if r == nil {
			req.Body = http.NoBody
			req.ContentLength = 0
			return req, nil
		}

		if rc, ok := r.(io.ReadCloser); ok {
			req.Body = rc
		} else {
			req.Body = ioutil.NopCloser(r)
		}

		if v, ok := r.(interface{ Len() int }); ok {
			req.ContentLength = int64(v.Len())
		} else if v, ok := r.(interface{ Size() int64 }); ok {
			req.ContentLength = v.Size()
		}

		return req, nil
	}
}

// set the request body as a JSON object
func JsonBody(body interface{}) RequestOption {
	return func(req *http.Request) (*http.Request, error) {
		b, err := simplejson.DumpBytes(body)
		if err != nil {
			return nil, err
		}
		req.Body = ioutil.NopCloser(bytes.NewBuffer(b))
		req.ContentLength = int64(len(b))
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		return req, nil
	}
}

// set the request body as a form object
func FormBody(params map[string]interface{}) RequestOption {
	return func(req *http.Request) (*http.Request, error) {
		data := ParamValues(params, nil)
		r := strings.NewReader(data.Encode())
		req.Body = ioutil.NopCloser(r)
		req.ContentLength = int64(r.Len())
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req, nil
	}
}

// set the Accept header
func Accept(ct string) RequestOption {
	return func(req *http.Request) (*http.Request, error) {
		req.Header.Set("Accept", ct)
		return req, nil
	}
}

// set the Content-Type header
func ContentType(ct string) RequestOption {
	return func(req *http.Request) (*http.Request, error) {
		req.Header.Set("Content-Type", ct)
		return req, nil
	}
}

// set the Content-Length header
func ContentLength(l int64) RequestOption {
	return func(req *http.Request) (*http.Request, error) {
		if l >= 0 {
			req.ContentLength = l
		}
		return req, nil
	}
}

// set specified HTTP headers
func Header(headers map[string]string) RequestOption {
	return func(req *http.Request) (*http.Request, error) {
		for k, v := range headers {
			if strings.ToLower(k) == "content-length" {
				if len, err := strconv.Atoi(v); err == nil && req.ContentLength <= 0 {
					req.ContentLength = int64(len)
				}
			} else if v == "" {
				req.Header.Del(k)
			} else {
				req.Header.Set(k, v)
			}
		}

		return req, nil
	}
}

// set request context
func Context(ctx context.Context) RequestOption {
	return func(req *http.Request) (*http.Request, error) {
		return req.WithContext(ctx), nil
	}
}

// set request ClientTrace
func Trace(tracer *httptrace.ClientTrace) RequestOption {
	return func(req *http.Request) (*http.Request, error) {
		return req.WithContext(httptrace.WithClientTrace(req.Context(), tracer)), nil
	}
}

/* func Close(close bool) RequestOption {
	return func(req *http.Request) error {
		req.Close = close
		return nil
	}
} */

// Execute request
func (self *HttpClient) SendRequest(options ...RequestOption) (*HttpResponse, error) {
	var path string
	if self.BaseURL != nil {
		path = self.BaseURL.String()
	}

	req, err := http.NewRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}

	req.Close = self.Close
	req.Host = self.Host

	self.addHeaders(req, nil)

	for _, opt := range options {
		if req, err = opt(req); err != nil {
			return nil, err
		}
	}

	return self.Do(req)
}

////////////////////////////////////////////////////////////////////////////////////
//
// Old style requests

// Execute request
func (self *HttpClient) Do(req *http.Request) (*HttpResponse, error) {
	var logClen string

	if req.Header.Get("Content-Length") == "" {
		logClen = fmt.Sprintf(" (Content-Length: %v)", req.ContentLength)
	}

	DebugLog(self.Verbose).Println("REQUEST:", req.Method, req.URL, pretty.PrettyFormat(req.Header)+logClen)

	resp, err := self.client.Do(req)
	if urlerr, ok := err.(*url.Error); ok && urlerr.Err == NoRedirect {
		err = nil // redirect on HEAD is not an error
	}
	if err == nil {
		DebugLog(self.Verbose).Println("RESPONSE:", resp.Status, pretty.PrettyFormat(resp.Header))
		return &HttpResponse{*resp}, nil
	} else {
		DebugLog(self.Verbose).Println("ERROR:", err,
			"REQUEST:", req.Method, req.URL,
			pretty.PrettyFormat(req.Header))
		CloseResponse(resp)
		return nil, err
	}
}

// Execute a DELETE request
func (self *HttpClient) Delete(path string, headers map[string]string) (*HttpResponse, error) {
	req := self.Request("DELETE", path, nil, headers)
	return self.Do(req)
}

// Execute a HEAD request
func (self *HttpClient) Head(path string, params map[string]interface{}, headers map[string]string) (*HttpResponse, error) {
	req := self.Request("HEAD", URLWithParams(path, params).String(), nil, headers)
	return self.Do(req)
}

// Execute a GET request
func (self *HttpClient) Get(path string, params map[string]interface{}, headers map[string]string) (*HttpResponse, error) {
	req := self.Request("GET", URLWithParams(path, params).String(), nil, headers)
	return self.Do(req)
}

// Execute a POST request
func (self *HttpClient) Post(path string, content io.Reader, headers map[string]string) (*HttpResponse, error) {
	req := self.Request("POST", path, content, headers)
	return self.Do(req)
}

func (self *HttpClient) PostForm(path string, data url.Values, headers map[string]string) (*HttpResponse, error) {
	if headers == nil {
		headers = map[string]string{}
	}
	headers["Content-Type"] = "application/x-www-form-urlencoded"
	req := self.Request("POST", path, strings.NewReader(data.Encode()), headers)
	return self.Do(req)
}

// Execute a PUT request
func (self *HttpClient) Put(path string, content io.Reader, headers map[string]string) (*HttpResponse, error) {
	req := self.Request("PUT", path, content, headers)
	return self.Do(req)
}

// Upload a file via form
func (self *HttpClient) UploadFile(method, path, fileParam, filePath string, payload []byte, params map[string]string, headers map[string]string) (*HttpResponse, error) {
	var reader io.Reader

	if payload == nil {
		file, err := os.Open(filePath)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		reader = file
	} else {
		reader = bytes.NewReader(payload)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile(fileParam, filepath.Base(filePath))
	if err == nil {
		_, err = io.Copy(part, reader)
	}
	if err == nil {
		for key, val := range params {
			writer.WriteField(key, val)
		}
		err = writer.Close()
	}
	if err != nil {
		return nil, err
	}

	if headers == nil {
		headers = map[string]string{}
	}

	headers["Content-Type"] = writer.FormDataContentType()
	headers["Content-Length"] = strconv.Itoa(body.Len())
	req := self.Request(method, path, body, headers)

	return self.Do(req)
}
