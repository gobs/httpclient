package httpclient

import (
        "bytes"
	"io/ioutil"
	"testing"
)

const (
        BASE_URL = "http://httpbin.org/"
	GET_URL  = BASE_URL + "get"
	POST_URL = BASE_URL + "post"
)

var (
	params = map[string]interface{}{
		"string": "one",
		"int":    2,
		"number": 3.14,
		"bool":   true,
		"list":   []string{"one", "two", "three"},
		"empty":  []int{},
	}
)

func TestURLWithParams(test *testing.T) {
	test.Log(URLWithParams(GET_URL, params))
}

func TestURLWithPathParams(test *testing.T) {
	test.Log(URLWithPathParams(GET_URL, "another", nil))
	test.Log(URLWithPathParams(GET_URL + "/", "another", nil))
}

func TestGet(test *testing.T) {
	resp, err := Get(GET_URL, nil)
	if err != nil {
		test.Error(err)
	} else {
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)
		test.Log(string(body))
	}
}

func TestGetWithParams(test *testing.T) {
	resp, err := Get(GET_URL, params)
	if err != nil {
		test.Error(err)
	} else {
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)
		test.Log(string(body))
	}
}

func TestPostWithParams(test *testing.T) {

	resp, err := Post(POST_URL, params)
	if err != nil {
		test.Error(err)
	} else {
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)
		test.Log(string(body))
	}
}

func TestGetJSON(test *testing.T) {
	resp, err := Get(GET_URL, params)
	if err != nil {
		test.Error(err)
	} else {
		test.Log(resp.Json().Map())
	}
}

func TestClient(test *testing.T) {
    client := NewHttpClient(BASE_URL)
    client.UserAgent = "TestClient 0.1"

    req := client.Request("GET", "get", nil)
    resp, err := client.Do(req)
    test.Log(err, string(resp.Content()))
    
    req = client.Request("POST", "post", bytes.NewBuffer([]byte("the body")))
    resp, err = client.Do(req)
    test.Log(err, string(resp.Content()))
}
