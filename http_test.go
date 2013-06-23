package http

import (
	"io/ioutil"
	"testing"
)

const (
	GET_URL  = "http://httpbin.org/get"
	POST_URL = "http://httpbin.org/post"
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
