package main

import (
	"io"
	"net/http"
	"time"
)

func main() {
	const target = "https://jsonplaceholder.typicode.com/users"
	client := &http.Client{Timeout: time.Second * 10}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		panic(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	// printHeader(resp)
	println(string(body))
}

func printHeader(resp *http.Response) {
	for key, values := range resp.Header {
		for _, value := range values {
			println(key, value)
		}
	}
}
