package httpclient

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"strconv"
)

// A transport that prints request and response

type LoggingTransport struct {
	t            *http.Transport
	requestBody  bool
	responseBody bool
}

func (lt *LoggingTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	dreq, _ := httputil.DumpRequest(req, lt.requestBody)
	fmt.Println("REQUEST:", strconv.Quote(string(dreq)))
	fmt.Println("")

	resp, err = lt.t.RoundTrip(req)
	if err != nil {
		fmt.Println("ERROR:", err)
	} else {
		dresp, _ := httputil.DumpResponse(resp, lt.responseBody)
		fmt.Println("RESPONSE:", string(dresp))
	}

	fmt.Println("")
	return
}

// Enable logging requests/response headers
//
// if requestBody == true, also log request body
// if responseBody == true, also log response body
func StartLogging(requestBody, responseBody bool) {
	http.DefaultTransport = &LoggingTransport{&http.Transport{}, requestBody, responseBody}
}

// Disable logging requests/responses
func StopLogging() {
	http.DefaultTransport = &http.Transport{}
}
