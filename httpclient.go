package httpclient

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gobs/pretty"
	"github.com/gobs/simplejson"
)

var (
	DefaultClient = &http.Client{} // we use our own default client, so we can change the TLS configuration

	NoRedirect       = errors.New("No redirect")
	TooManyRedirects = errors.New("stopped after 10 redirects")
)

//
// Allow connections via HTTPS even if something is wrong with the certificate
// (self-signed or expired)
//
func AllowInsecure(insecure bool) {
	if insecure {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}

		DefaultClient.Transport = tr
	} else {
		DefaultClient.Transport = nil
	}
}

//
// Set connection timeout
//
func SetTimeout(t time.Duration) {
	DefaultClient.Timeout = t
}

//
// HTTP error
//
type HttpError struct {
	Code    int
	Message string
}

func (e HttpError) Error() string {
	return e.Message
}

func (e HttpError) String() string {
	return fmt.Sprintf("ERROR: %v %v", e.Code, e.Message)
}

//
// CloseResponse makes sure we close the response body
//
func CloseResponse(r *http.Response) {
	if r != nil && r.Body != nil {
		io.Copy(ioutil.Discard, r.Body)
		r.Body.Close()
	}
}

//
// A wrapper for http.Response
//
type HttpResponse struct {
	http.Response
}

func (r *HttpResponse) ContentType() string {
	content_type := r.Header.Get("Content-Type")
	if len(content_type) == 0 {
		return content_type
	}

	return strings.TrimSpace(strings.Split(content_type, ";")[0])
}

//
// Close makes sure that all data from the body is read
// before closing the reader.
//
// If that is not the desider behaviour, just call HttpResponse.Body.Close()
//
func (r *HttpResponse) Close() {
        if r != nil {
	    CloseResponse(&r.Response)
        }
}

//
// ResponseError checks the StatusCode and return an error if needed.
// The error is of type HttpError
//
func (r *HttpResponse) ResponseError() error {
	class := r.StatusCode / 100
	if class != 2 && class != 3 {
		return HttpError{Code: r.StatusCode, Message: "HTTP " + r.Status}
	}

	return nil
}

//
// Check if the input value is a "primitive" that can be safely stringified
//
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

//
// Given a base URL and a bag of parameteters returns the URL with the encoded parameters
//
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

	q := u.Query()

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

	u.RawQuery = q.Encode()
	return u
}

func URLWithParams(base string, params map[string]interface{}) (u *url.URL) {
	return URLWithPathParams(base, "", params)
}

//
// http.Get with params
//
func Get(urlStr string, params map[string]interface{}) (*HttpResponse, error) {
	resp, err := DefaultClient.Get(URLWithParams(urlStr, params).String())
	if err == nil {
		return &HttpResponse{*resp}, nil
	} else {
		CloseResponse(resp)
		return nil, err
	}
}

//
// http.Post with params
//
func Post(urlStr string, params map[string]interface{}) (*HttpResponse, error) {
	resp, err := DefaultClient.PostForm(urlStr, URLWithParams(urlStr, params).Query())
	if err == nil {
		return &HttpResponse{*resp}, nil
	} else {
		CloseResponse(resp)
		return nil, err
	}
}

//
//  Read the body
//
func (resp *HttpResponse) Content() []byte {
	body, _ := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	return body
}

//
//  Try to parse the response body as JSON
//
func (resp *HttpResponse) Json() (json *simplejson.Json) {
	json, _ = simplejson.LoadBytes(resp.Content())
	return
}

////////////////////////////////////////////////////////////////////////

//
// http.Client with some defaults and stuff
//
type HttpClient struct {
	// the http.Client
	client *http.Client

	// the base URL for this client
	BaseURL *url.URL

	// the client UserAgent string
	UserAgent string

	// Common headers to be passed on each request
	Headers map[string]string

	// if Verbose, log request and response info
	Verbose bool
}

//
// Create a new HttpClient
//
func NewHttpClient(base string) (httpClient *HttpClient) {
	httpClient = new(HttpClient)
	httpClient.client = &http.Client{CheckRedirect: httpClient.checkRedirect}
	httpClient.Headers = make(map[string]string)

	if u, err := url.Parse(base); err != nil {
		log.Fatal(err)
	} else {
		httpClient.BaseURL = u
	}

	return
}

//
// Allow connections via HTTPS even if something is wrong with the certificate
// (self-signed or expired)
//
func (self *HttpClient) AllowInsecure(insecure bool) {
	if insecure {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}

		self.client.Transport = tr
	} else {
		self.client.Transport = nil
	}
}

//
// Set connection timeout
//
func (self *HttpClient) SetTimeout(t time.Duration) {
	self.client.Timeout = t
}

//
// Set LocalAddr in Dialer
//
func (self *HttpClient) SetLocalAddr(addr string) {
	transport, ok := self.client.Transport.(*http.Transport)
	if transport == nil {
		if transport, ok = http.DefaultTransport.(*http.Transport); !ok {
			log.Println("SetLocalAddr for http.DefaultTransport != http.Transport")
			return
		}
	} else if !ok {
		log.Println("SetLocalAddr for client.Transport != http.Transport")
		return
	}
	if tcpaddr, err := net.ResolveTCPAddr("tcp", addr); err == nil {
		transport.Dial = (&net.Dialer{
			Timeout:   30 * time.Second, // defaults from net/http DefaultTransport
			KeepAlive: 30 * time.Second, // defaults from net/http DefaultTransport
			LocalAddr: tcpaddr,
		}).Dial
	} else {
		log.Println("Failed to resolve", addr, " to a TCP address")
	}
}

//
// add default headers plus extra headers
//
func (self *HttpClient) addHeaders(req *http.Request, headers map[string]string) {

	if len(self.UserAgent) > 0 {
		req.Header.Set("User-Agent", self.UserAgent)
	}

	for k, v := range self.Headers {
		req.Header.Set(k, v)
	}

	for k, v := range headers {
		if strings.ToLower(k) == "content-length" {
			if len, err := strconv.Atoi(v); err == nil && req.ContentLength <= 0 {
				req.ContentLength = int64(len)
			}
		} else {
			req.Header.Set(k, v)
		}
	}

}

//
// the callback for CheckRedirect, used to pass along the headers in case of redirection
//
func (self *HttpClient) checkRedirect(req *http.Request, via []*http.Request) error {
	if req.Method == "HEAD" {
		// don't follow redirects on a HEAD request
		return NoRedirect
	}

	if self.Verbose {
		log.Println("REDIRECT:", len(via), req.URL)
	}

	if len(via) >= 10 {
		return TooManyRedirects
	}

	// TODO: check for same host before adding headers
	self.addHeaders(req, nil)
	return nil
}

//
// Create a request object given the method, path, body and extra headers
//
func (self *HttpClient) Request(method string, urlpath string, body io.Reader, headers map[string]string) (req *http.Request) {
	if u, err := self.BaseURL.Parse(urlpath); err != nil {
		log.Fatal(err)
	} else {
		urlpath = u.String()
	}

	req, err := http.NewRequest(method, urlpath, body)
	if err != nil {
		log.Fatal(err)
	}

	self.addHeaders(req, headers)
	return
}

//
// Execute request
//
func (self *HttpClient) Do(req *http.Request) (*HttpResponse, error) {
	if self.Verbose {
		log.Println("REQUEST:", req.Method, req.URL, pretty.PrettyFormat(req.Header))
	}

	resp, err := self.client.Do(req)
	if urlerr, ok := err.(*url.Error); ok && urlerr.Err == NoRedirect {
		err = nil // redirect on HEAD is not an error
	}
	if err == nil {
		if self.Verbose {
			log.Println("RESPONSE:", resp.Status, pretty.PrettyFormat(resp.Header))
		}

		return &HttpResponse{*resp}, nil
	} else {
		if self.Verbose {
			log.Println("ERROR:", err, "REQUEST:", req.Method, req.URL, pretty.PrettyFormat(req.Header))
		}

		CloseResponse(resp)
		return nil, err
	}
}

//
// Execute a DELETE request
//
func (self *HttpClient) Delete(path string, headers map[string]string) (*HttpResponse, error) {
	req := self.Request("DELETE", path, nil, headers)
	return self.Do(req)
}

//
// Execute a HEAD request
//
func (self *HttpClient) Head(path string, params map[string]interface{}, headers map[string]string) (*HttpResponse, error) {
	req := self.Request("HEAD", URLWithParams(path, params).String(), nil, headers)
	return self.Do(req)
}

//
// Execute a GET request
//
func (self *HttpClient) Get(path string, params map[string]interface{}, headers map[string]string) (*HttpResponse, error) {
	req := self.Request("GET", URLWithParams(path, params).String(), nil, headers)
	return self.Do(req)
}

//
// Execute a POST request
//
func (self *HttpClient) Post(path string, content io.Reader, headers map[string]string) (*HttpResponse, error) {
	req := self.Request("POST", path, content, headers)
	return self.Do(req)
}

//
// Execute a PUT request
//
func (self *HttpClient) Put(path string, content io.Reader, headers map[string]string) (*HttpResponse, error) {
	req := self.Request("PUT", path, content, headers)
	return self.Do(req)
}
