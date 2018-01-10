package httpclient

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"time"
)

// A transport that prints request and response

type LoggingTransport struct {
	t            *http.Transport
	requestBody  bool
	responseBody bool
	timing       bool
}

func (lt *LoggingTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	dreq, _ := httputil.DumpRequest(req, lt.requestBody)
	//fmt.Println("REQUEST:", strconv.Quote(string(dreq)))
	fmt.Println("REQUEST:", string(dreq))
	fmt.Println("")

	var startTime time.Time
	var elapsed time.Duration

	if lt.timing {
		startTime = time.Now()
	}

	resp, err = lt.t.RoundTrip(req)

	if lt.timing {
		elapsed = time.Since(startTime)
	}

	if err != nil {
		if lt.requestBody {
			// don't print the body twice
			dreq, _ = httputil.DumpRequest(req, false)
		}
		fmt.Println("ERROR:", err, "REQUEST:", strconv.Quote(string(dreq)))
	}
	if resp != nil {
		dresp, _ := httputil.DumpResponse(resp, lt.responseBody)
		fmt.Println("RESPONSE:", string(dresp))
	}

	if elapsed > 0 {
		fmt.Println("ELAPSED TIME:", elapsed.Round(time.Millisecond))
	}

	fmt.Println("")
	return
}

func (lt *LoggingTransport) CancelRequest(req *http.Request) {
	dreq, _ := httputil.DumpRequest(req, false)
	fmt.Println("CANCEL REQUEST:", strconv.Quote(string(dreq)))
	lt.t.CancelRequest(req)
}

// Enable logging requests/response headers
//
// if requestBody == true, also log request body
// if responseBody == true, also log response body
// if timing == true, also log elapsed time
func StartLogging(requestBody, responseBody, timing bool) {
	http.DefaultTransport = &LoggingTransport{&http.Transport{}, requestBody, responseBody, timing}
}

// Disable logging requests/responses
func StopLogging() {
	http.DefaultTransport = &http.Transport{}
}

// Wrap input transport into a LoggingTransport
func LoggedTransport(t *http.Transport, requestBody, responseBody, timing bool) http.RoundTripper {
	return &LoggingTransport{t, requestBody, responseBody, timing}
}

// A Reader that "logs" progress

type ProgressReader struct {
	r         io.Reader
	c         [1]byte
	threshold int
	curr      int
}

func NewProgressReader(r io.Reader, c byte, threshold int) *ProgressReader {
	if c == 0 {
		c = '.'
	}
	if threshold <= 0 {
		threshold = 10240
	}
	p := &ProgressReader{r: r, c: [1]byte{c}, threshold: threshold, curr: 0}
	return p
}

func (p *ProgressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)

	p.curr += n

	if err == io.EOF {
		os.Stdout.Write([]byte{'\n'})
		os.Stdout.Sync()
	} else if p.curr >= p.threshold {
		p.curr -= p.threshold

		os.Stdout.Write(p.c[:])
		os.Stdout.Sync()
	}

	return n, err
}

func (p *ProgressReader) Close() error {
	if rc, ok := p.r.(io.ReadCloser); ok {
		return rc.Close()
	} else {
		return nil
	}
}

type DebugLog bool

func (d DebugLog) Println(args ...interface{}) {
	if d {
		log.Println(args...)
	}
}

func (d DebugLog) Printf(fmt string, args ...interface{}) {
	if d {
		log.Printf(fmt, args...)
	}
}
