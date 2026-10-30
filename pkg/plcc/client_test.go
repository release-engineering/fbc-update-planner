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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/avast/retry-go/v4"
)

// mockRetry disables retry backoff delays for the duration of the test.
func mockRetry(t *testing.T) {
	t.Helper()
	original := retryOptions
	retryOptions = append([]retry.Option(nil), append(original, retry.Delay(0))...)
	t.Cleanup(func() { retryOptions = original })
}

func TestFetchFrom(t *testing.T) {
	catalog := &Catalog{Data: []Product{
		{Name: "Test Product", Package: "test-pkg", Versions: []Version{
			{Name: "1.0", Phases: []Phase{{Name: "GA", StartDate: "2025-01-01T00:00:00.000Z", EndDate: "2025-12-31T00:00:00.000Z"}}},
		}},
	}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(catalog); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer srv.Close()

	for _, name := range []string{"legacy", "context"} {
		t.Run(name, func(t *testing.T) {
			var got *Catalog
			var err error
			if name == "legacy" {
				got, err = FetchFrom(srv.URL, srv.Client())
			} else {
				got, err = FetchFromContext(t.Context(), srv.URL, srv.Client())
			}
			if err != nil {
				t.Fatalf("fetch failed: %v", err)
			}
			if len(got.Data) != 1 {
				t.Fatalf("got %d products, want 1", len(got.Data))
			}
			if got.Data[0].Package != "test-pkg" {
				t.Errorf("got package %q, want %q", got.Data[0].Package, "test-pkg")
			}
		})
	}
}

func TestFetchFromContextAlreadyCanceled(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := FetchFromContext(ctx, srv.URL, srv.Client())
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("fetch = %+v, %v; want nil, context.Canceled", got, err)
	}
	if attempts.Load() != 0 {
		t.Fatal("canceled fetch sent an HTTP request")
	}
}

func TestFetchFromContextCancellation(t *testing.T) {
	for _, stage := range []string{"request", "response body", "retry backoff"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			started := make(chan struct{}, 3)
			release := make(chan struct{})
			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				if stage == "retry backoff" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if stage == "response body" {
					_, _ = io.WriteString(w, `{"data":[`)
					w.(http.Flusher).Flush()
				}
				started <- struct{}{}
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer srv.Close()
			defer close(release)
			if stage == "retry backoff" {
				original := retryOptions
				retryOptions = append(append([]retry.Option(nil), original...), retry.DelayType(func(uint, error, *retry.Config) time.Duration {
					started <- struct{}{}
					return time.Hour
				}))
				defer func() { retryOptions = original }()
			}
			type result struct {
				catalog *Catalog
				err     error
			}
			done := make(chan result, 1)
			go func() {
				catalog, err := FetchFromContext(ctx, srv.URL, srv.Client())
				done <- result{catalog, err}
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatalf("fetch did not reach %s", stage)
			}
			cancel()
			select {
			case got := <-done:
				if got.catalog != nil || !errors.Is(got.err, context.Canceled) {
					t.Fatalf("fetch = %+v, %v; want nil, context.Canceled", got.catalog, got.err)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("cancellation did not interrupt %s", stage)
			}
			if attempts.Load() != 1 {
				t.Fatalf("canceled fetch made %d HTTP attempts, want 1", attempts.Load())
			}
		})
	}
}

func TestFetchFromHTTPError(t *testing.T) {
	mockRetry(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := FetchFrom(srv.URL, srv.Client())
	if err == nil {
		t.Fatal("expected error for HTTP 404, got nil")
	}
}

func TestFetchFromHTTPErrorRetries(t *testing.T) {
	mockRetry(t)
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := FetchFrom(srv.URL, srv.Client())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestFetchFromRetry(t *testing.T) {
	mockRetry(t)
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(&Catalog{Data: []Product{{Package: "retry-pkg"}}}); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer srv.Close()

	got, err := FetchFrom(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("FetchFrom failed after retries: %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
	if got.Data[0].Package != "retry-pkg" {
		t.Errorf("got package %q, want %q", got.Data[0].Package, "retry-pkg")
	}
}
