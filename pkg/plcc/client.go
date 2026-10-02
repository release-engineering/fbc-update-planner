/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package plcc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/avast/retry-go/v4"
)

// APIURL is the Red Hat Product Life Cycle API endpoint.
const APIURL = "https://access.redhat.com/product-life-cycles/api/v2/products"

// Fetch retrieves the product catalog from the default PLCC API endpoint.
func Fetch() (*Catalog, error) {
	return FetchContext(context.Background())
}

// FetchContext retrieves the product catalog from the default PLCC API endpoint.
// The context cancels HTTP requests, response reads, and retry delays.
func FetchContext(ctx context.Context) (*Catalog, error) {
	return FetchFromContext(ctx, APIURL, &http.Client{Timeout: 30 * time.Second})
}

var retryOptions = []retry.Option{
	retry.Attempts(3),
	retry.Delay(60 * time.Second),
	retry.DelayType(retry.BackOffDelay),
	retry.LastErrorOnly(true),
}

// FetchFrom retrieves the product catalog from the given URL using the provided HTTP client.
// It makes up to 3 attempts with exponential backoff on errors.
func FetchFrom(url string, client *http.Client) (*Catalog, error) {
	return FetchFromContext(context.Background(), url, client)
}

// FetchFromContext retrieves the product catalog using the given URL and client.
// It makes up to 3 attempts with exponential backoff on errors. The context
// cancels HTTP requests, response reads, and retry delays.
func FetchFromContext(ctx context.Context, url string, client *http.Client) (*Catalog, error) {
	opts := append([]retry.Option(nil), retryOptions...)
	opts = append(opts, retry.Context(ctx))
	catalog, err := retry.DoWithData(func() (*Catalog, error) {
		return fetch(ctx, url, client)
	}, opts...)
	if err != nil {
		return nil, fmt.Errorf("after retries: %w", err)
	}
	return catalog, nil
}

func fetch(ctx context.Context, url string, client *http.Client) (*Catalog, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create HTTP request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	var catalog Catalog
	if err := json.Unmarshal(body, &catalog); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &catalog, nil
}

// Load reads the product catalog from a local JSON file.
func Load(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading PLCC file: %w", err)
	}
	var catalog Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("decoding PLCC file: %w", err)
	}
	return &catalog, nil
}
