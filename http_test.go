package http

import (
	"testing"
	"fmt"

	)

func TestURLWithParams(*testing.T) {
	
	params := map[string]interface{} {
		"string": "one",
		"int": 2,
		"number": 3.14,
		"bool": true,
		"list": []string { "one", "two", "three" },
		"empty": []int {},
	}

	fmt.Println(URLWithParams("http://testing.com", params))
}
