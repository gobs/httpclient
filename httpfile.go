package httpclient

import (
	"fmt"
	"io"
	"net/http"
	"os"
)

// HttpFile is a file-like object that allows reading and seeking from an
// http resources (via an HTTP GET with Range request)
type HttpFile struct {
	Url     string
	Headers map[string]string

	client *http.Client
	pos    int64
	len    int64
}

// HttpFileError wraps a network error
type HttpFileError struct {
	Err error
}

func (e *HttpFileError) Error() string {
	return "HttpFileError: " + e.Err.Error()
}

// Creates an HttpFile object. At this point the "file" is "open"
func OpenHttpFile(url string, headers map[string]string) (*HttpFile, error) {
	f := HttpFile{Url: url, Headers: headers, client: &http.Client{}, pos: 0, len: -1}

	resp, err := f.do("HEAD", nil)
	defer resp.Body.Close()

	if err != nil {
		return nil, &HttpFileError{Err: err}
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, os.ErrNotExist
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &HttpFileError{Err: fmt.Errorf("Unexpected Status %s", resp.Status)}
	}

	f.len = resp.ContentLength
	return &f, nil
}

type headers map[string]string

func (f *HttpFile) do(method string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequest(method, f.Url, nil)
	if err != nil {
		return nil, err
	}

	for k, v := range f.Headers {
		req.Header.Set(k, v)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return f.client.Do(req)
}

// The Reader interface
func (f *HttpFile) Read(p []byte) (int, error) {
	if f.client == nil {
		return 0, os.ErrInvalid
	}

	plen := len(p)
	if plen <= 0 {
		return plen, nil
	}

	bytes_range := fmt.Sprintf("bytes=%d-%d", f.pos, f.pos+int64(plen-1))
	resp, err := f.do("GET", headers{"Range": bytes_range})
	defer resp.Body.Close()

	if err != nil {
		return 0, &HttpFileError{Err: err}
	}
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		return 0, io.EOF
	}
	if resp.StatusCode != http.StatusPartialContent {
		return 0, &HttpFileError{Err: fmt.Errorf("Unexpected Status %s", resp.Status)}
	}

	content_range := resp.Header.Get("Content-Range")

	var first, last, total int64
	n, err := fmt.Sscanf(content_range, "bytes %d-%d/%d", &first, &last, &total)
	if err != nil {
		return 0, err
	}
	if n != 3 {
		return 0, &HttpFileError{Err: fmt.Errorf("Unexpected Content-Range %q (%d)", content_range, n)}
	}

	r, err := resp.Body.Read(p)
	if err != nil {
		return 0, err
	}

	f.pos += int64(r)
	return r, nil
}

// The Closer interface
func (f *HttpFile) Close() error {
	f.client = nil
	f.pos = -1
	f.len = -1
	return nil
}

// The Seeker interface
func (f *HttpFile) Seek(offset int64, whence int) (int64, error) {
	var newpos int64 = -1

	if f.client != nil {
		switch whence {
		case 0: // from 0
			newpos = offset

		case 1: // from current
			newpos = f.pos + offset

		case 2: // from end
			newpos = f.len + offset
		}
	}

	if newpos < 0 {
		return 0, os.ErrInvalid
	} else {
		f.pos = newpos
		return f.pos, nil
	}
}
