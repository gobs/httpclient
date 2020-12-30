package httpclient

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// HttpFile is a file-like object that allows reading and seeking from an
// http resources (via an HTTP GET with Range request)
type HttpFile struct {
	Url     string
	Headers map[string]string
	Debug   bool

	Buffer []byte

	origUrl string
	client  *http.Client
	pos     int64
	len     int64

	bpos   int64 // seek position for buffered reads
	bstart int   // first available byte in buffer
	bend   int   // last available byte in buffer
}

// HttpFileError wraps a network error
type HttpFileError struct {
	Err error
}

func (e *HttpFileError) Error() string {
	return "HttpFileError: " + e.Err.Error()
}

func (e *HttpFileError) Temporary() bool {
	if ue, ok := e.Err.(*url.Error); ok {
		return ue.Temporary()
	}

	return false
}

func (e *HttpFileError) Timeout() bool {
	if ue, ok := e.Err.(*url.Error); ok {
		return ue.Timeout()
	}

	return false
}

type headersType map[string]string

var HttpFileNoHead = false
var HttpFileRetries = 10
var HttpFileRetryWait = 60 * time.Second

// Creates an HttpFile object. At this point the "file" is "open"
func OpenHttpFile(url string, headers map[string]string) (*HttpFile, error) {
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return NoRedirect
		},
	}

	f := HttpFile{Url: url, Headers: headers, origUrl: url, client: client, pos: 0, len: -1}

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
retry_redir:
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

	redirect := false
	retry := 0

	for {
		res, err := f.client.Do(req)
		if uerr, ok := err.(*url.Error); ok && uerr.Err == NoRedirect {
			if redirect { // we already redirected once
				return res, err
			}

			redirect = true
			f.Url = res.Header.Get("Location")
			goto retry_redir
		}

		if err != nil {
			return res, err
		}

		if res.StatusCode == 403 {
			if res.Header.Get("X-Cache") == "Error from cloudfront" {
				log.Println(req, err)

				retry++

				if retry < HttpFileRetries {
					log.Println("Retry", retry, "Sleep...")
					time.Sleep(HttpFileRetryWait)
					continue
				}
			} else if res.Header.Get("X-AMZ-Request-ID") != "" {
				var buf [256]byte
				n, err := res.Body.Read(buf[:])
				if err == nil {
					errbody := string(buf[:n])

					log.Println(req, err, errbody)

					if strings.Contains(errbody, `<Message>Request has expired</Message>`) &&
						f.Url != f.origUrl { // retry redirect
						log.Println("Retry redirect")
						f.Url = f.origUrl
						goto retry_redir
					}
				}
			}
		}

		return res, err
	}
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

func (f *HttpFile) readAt(p []byte, off int64) (int, error) {
	DebugLog(f.Debug).Println("readAt", off, len(p))

	if f.client == nil {
		return 0, os.ErrInvalid
	}

	plen := len(p)
	if plen <= 0 {
		return plen, nil
	}

	end := off + int64(plen)
	if end > f.len {
		end = f.len
	}

	bytes_range := fmt.Sprintf("bytes=%d-%d", off, end-1)
	resp, err := f.do("GET", headersType{"Range": bytes_range})
	defer CloseResponse(resp)

	switch {
	case err != nil:
		DebugLog(f.Debug).Println("readAt error", err)
		return 0, &HttpFileError{Err: err}

	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		DebugLog(f.Debug).Println("readAt http.StatusRequestedRangeNotSatisfiable")
		return 0, io.EOF

	case resp.StatusCode != http.StatusPartialContent:
		DebugLog(f.Debug).Println("readAt error", resp.Status)
		return 0, &HttpFileError{Err: fmt.Errorf("Unexpected Status %s", resp.Status)}
	}

	first, last, total, err := f.getContentRange(resp)
	DebugLog(f.Debug).Println("Range", bytes_range, "Content-Range", first, last, total)

	n, err := io.ReadFull(resp.Body, p)
	if n > 0 && err == io.EOF {
		// read reached EOF, but archive/zip doesn't like this!
		DebugLog(f.Debug).Println("readAt", n, "reached EOF")
		err = nil
	}

	DebugLog(f.Debug).Println("readAt", n, err)
	return n, err
}

func (f *HttpFile) readFromBuffer(p []byte, off int64) (int, error) {
	ppos := 0
	plen := len(p)

	if plen == 0 {
		DebugLog(f.Debug).Println("readFrom", off, "zero bbuffer")
		return 0, nil
	}

	if off != f.bpos {
		blen := f.bend - f.bstart

		if blen == 0 {
			f.bstart = 0
			f.bend = 0
		} else if f.bpos < off && f.bpos+int64(blen) > off {
			drop := int(off - f.bpos)
			f.bstart += drop

			DebugLog(f.Debug).Println("readFrom", off, "pos", f.bpos, "drop", drop, "bytes, saved", blen-drop, "bytes")
		} else {
			DebugLog(f.Debug).Println("readFrom", off, "pos", f.bpos, "dropping", blen, "bytes")

			f.bstart = 0
			f.bend = 0
		}

		f.bpos = off
	}

	for ppos < plen {
		DebugLog(f.Debug).Println("readFromBuffer", ppos, plen, "pos", f.bpos)

		if f.bstart < f.bend { // there is already some data
			n := copy(p[ppos:], f.Buffer[f.bstart:f.bend])

			f.bstart += n
			f.bpos += int64(n)
			ppos += n

			if ppos >= plen {
				DebugLog(f.Debug).Println("readFromBuffer", ppos, "done", "pos", f.bpos)
				return ppos, nil
			}
		}

		if plen-ppos > len(f.Buffer) { // no need to read in buffer
			f.bstart = 0
			f.bend = 0

			return f.readAt(p[ppos:], f.bpos)
		}

		n, err := f.readAt(f.Buffer, f.bpos)

		f.bstart = 0
		f.bend = n

		if err != nil && n == 0 { // don't return an error if we read something
			DebugLog(f.Debug).Println("readFromBuffer", "error", err)
			return 0, err
		}
	}

	log.Println("ppos", ppos, "plen", plen, "bstart", f.bstart, "bend", f.bend)

	panic("should not get here")
	return 0, nil
}

// The ReaderAt interface
func (f *HttpFile) ReadAt(p []byte, off int64) (int, error) {
	DebugLog(f.Debug).Println("ReadAt", off, "len", len(p))

	if f.Buffer != nil {
		return f.readFromBuffer(p, off)
	}

	return f.readAt(p, off)
}

// The Reader interface
func (f *HttpFile) Read(p []byte) (int, error) {
	DebugLog(f.Debug).Println("Read from", f.pos, "len", len(p))

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
		if f.pos != newpos {
			f.pos = newpos
		}

		return f.pos, nil
	}
}
