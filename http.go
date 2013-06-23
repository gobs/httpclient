package http

import (
	"fmt"
	"log"
	"net/url"

	gohttp "net/http"

	"reflect"
)

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
func URLWithParams(base string, params map[string]interface{}) (u *url.URL) {

	u, err := url.Parse(base)
	if err != nil {
		log.Fatal(err)
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

//
// http.Get with params
//
func Get(url string, params map[string]interface{}) (*gohttp.Response, error) {
	return gohttp.Get(URLWithParams(url, params).String())
}

//
// http.Post with params
//
func Post(url string, params map[string]interface{}) (*gohttp.Response, error) {
	return gohttp.PostForm(url, URLWithParams(url, params).Query())
}
