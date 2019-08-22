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
	Debug   bool

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

type headersType map[string]string

var HttpFileNoHead = false

// Creates an HttpFile object. At this point the "file" is "open"
func OpenHttpFile(url string, headers map[string]string) (*HttpFile, error) {
	f := HttpFile{Url: url, Headers: headers, client: &http.Client{}, pos: 0, len: -1}

	hmethod := "HEAD"
	var hheaders map[string]string

	if HttpFileNoHead { // some servers don't support HEAD, try with a GET of 0 bytes (actually 1)
		hmethod = "GET"
		hheaders = headersType{"Range": "bytes=0-0"}
	}

	resp, err := f.do(hmethod, hheaders)
	defer CloseResponse(resp)

	if err != nil {
		return nil, &HttpFileError{Err: err}
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, os.ErrNotExist
	}
	if resp.StatusCode == http.StatusOK {
		f.len = resp.ContentLength
	} else if resp.StatusCode == http.StatusPartialContent {
		_, _, clen, err := f.getContentRange(resp)
		if err != nil {
			return nil, err
		}

		f.len = clen
	} else {
		return nil, &HttpFileError{Err: fmt.Errorf("Unexpected Status %s", resp.Status)}
	}

	return &f, nil
}

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

func (f *HttpFile) getContentRange(resp *http.Response) (first, last, total int64, err error) {
	content_range := resp.Header.Get("Content-Range")

	n, err := fmt.Sscanf(content_range, "bytes %d-%d/%d", &first, &last, &total)
	if err != nil {
		DebugLog(f.Debug).Println("Error", err)
		return -1, -1, -1, err
	}
	if n != 3 {
		return -1, -1, -1, &HttpFileError{Err: fmt.Errorf("Unexpected Content-Range %q (%d)", content_range, n)}
	}

	return first, last, total, nil
}

// Returns the file size
func (f *HttpFile) Size() int64 {
	DebugLog(f.Debug).Println("Size", f.len)
	return f.len
}

// The ReaderAt interface
func (f *HttpFile) ReadAt(p []byte, off int64) (int, error) {
	DebugLog(f.Debug).Println("ReadAt", off, len(p))

	if f.client == nil {
		return 0, os.ErrInvalid
	}

	plen := len(p)
	if plen <= 0 {
		return plen, nil
	}

	bytes_range := fmt.Sprintf("bytes=%d-%d", off, off+int64(plen-1))
	resp, err := f.do("GET", headersType{"Range": bytes_range})
	defer CloseResponse(resp)

	if err != nil {
		DebugLog(f.Debug).Println("ReadAt error", err)
		return 0, &HttpFileError{Err: err}
	}
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		return 0, io.EOF
	}
	if resp.StatusCode != http.StatusPartialContent {
		return 0, &HttpFileError{Err: fmt.Errorf("Unexpected Status %s", resp.Status)}
	}

	first, last, total, err := f.getContentRange(resp)
	DebugLog(f.Debug).Println("Range", bytes_range, "Content-Range", first, last, total)

	n, err := io.ReadFull(resp.Body, p)
	if n > 0 && err == io.EOF {
		// read reached EOF, but archive/zip doesn't like this!
		DebugLog(f.Debug).Println("Read", n, "reached EOF")
		err = nil
	}

	DebugLog(f.Debug).Println("Read", n, err)
	return n, err
}

// The Reader interface
func (f *HttpFile) Read(p []byte) (int, error) {

	n, err := f.ReadAt(p, f.pos)
	if n > 0 {
		f.pos += int64(n)
	}

	DebugLog(f.Debug).Println("Read", n, err)
	return n, err
}

// The Closer interface
func (f *HttpFile) Close() error {
	DebugLog(f.Debug).Println("Close")
	f.client = nil
	f.pos = -1
	f.len = -1
	return nil
}

// The Seeker interface
func (f *HttpFile) Seek(offset int64, whence int) (int64, error) {
	DebugLog(f.Debug).Println("Seek", offset, whence)

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
