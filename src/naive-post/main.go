package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func main() {
	const target = "https://jsonplaceholder.typicode.com/posts"

	client := &http.Client{Timeout: 10 * time.Second}

	postData, err := json.Marshal(map[string]any{
		"title":  "My Post",
		"body":   "Content",
		"userId": 1,
	})
	if err != nil {
		panic(err)
	}
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewBuffer(postData))
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
	fmt.Println(string(body))
}

func printHeader(resp *http.Response) {
	for key, values := range resp.Header {
		for _, value := range values {
			fmt.Println(key, value)
		}
	}
}
