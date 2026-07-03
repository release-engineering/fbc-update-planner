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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// PackagesNotFoundError is returned when requested package names are not found in the catalog.
type PackagesNotFoundError struct {
	Names []string
}

func (e *PackagesNotFoundError) Error() string {
	return fmt.Sprintf("packages not found in PLCC data: %s", strings.Join(e.Names, ", "))
}

// APIURL is the Red Hat Product Life Cycle API endpoint.
const APIURL = "https://access.redhat.com/product-life-cycles/api/v2/products"

// Catalog holds the product lifecycle data returned by the PLCC API.
type Catalog struct {
	Data []Product `json:"data"`
}

// Product represents a software product with its lifecycle versions.
type Product struct {
	Name           string    `json:"name"`
	Package        string    `json:"package"`
	Versions       []Version `json:"versions"`
	ReleaseCadence string    `json:"release_cadence"`
	IsOperator     bool      `json:"is_operator"`
}

// Version represents a product version with its lifecycle phases and platform compatibility.
type Version struct {
	Name                   string  `json:"name"`
	Phases                 []Phase `json:"phases"`
	OpenShiftCompatibility string  `json:"openshift_compatibility"`
	Tier                   string  `json:"tier"`
}

// Phase represents a lifecycle phase with start and end dates (ISO8601 timestamps).
type Phase struct {
	Name            string `json:"name"`
	StartDate       string `json:"start_date"`
	EndDate         string `json:"end_date"`
	StartDateFormat string `json:"start_date_format"`
	EndDateFormat   string `json:"end_date_format"`
}

// Fetch retrieves the product catalog from the default PLCC API endpoint.
func Fetch() (*Catalog, error) {
	return FetchFrom(APIURL, &http.Client{Timeout: 30 * time.Second})
}

var sleepFunc = time.Sleep

// FetchFrom retrieves the product catalog from the given URL using the provided HTTP client.
// It retries up to 3 times with exponential backoff on errors.
func FetchFrom(url string, client *http.Client) (*Catalog, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := range maxRetries {
		if attempt > 0 {
			// attempt 1: 60s, attempt 2: 120s
			sleepFunc(time.Duration(60<<(attempt-1)) * time.Second)
		}

		catalog, err := fetch(url, client)
		if err == nil {
			return catalog, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("after %d attempts: %w", maxRetries, lastErr)
}

func fetch(url string, client *http.Client) (*Catalog, error) {
	resp, err := client.Get(url)
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

// FilterPackages removes products that have no package name, modifying the catalog in place.
func (c *Catalog) FilterPackages() {
	filtered := make([]Product, 0, len(c.Data))
	for _, p := range c.Data {
		if p.Package != "" {
			filtered = append(filtered, p)
		}
	}
	c.Data = filtered
}

// FilterByPackageNames keeps only products whose package name is in the provided list,
// modifying the catalog in place. It returns a PackagesNotFoundError if any names were not found.
// The catalog is modified in place also in case of error.
func (c *Catalog) FilterByPackageNames(names []string) error {
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name] = true
	}
	found := make(map[string]bool, len(names))
	filtered := make([]Product, 0, len(names))
	for _, p := range c.Data {
		if allowed[p.Package] {
			filtered = append(filtered, p)
			found[p.Package] = true
		}
	}
	c.Data = filtered

	var notFound []string
	for _, name := range names {
		if !found[name] {
			notFound = append(notFound, name)
		}
	}
	if len(notFound) > 0 {
		return &PackagesNotFoundError{Names: notFound}
	}
	return nil
}

// Len returns the number of products currently in the catalog.
func (c *Catalog) Len() int {
	return len(c.Data)
}

// SortByPackage sorts products by package name in ascending order.
func (c *Catalog) SortByPackage() {
	sort.Slice(c.Data, func(i, j int) bool {
		return c.Data[i].Package < c.Data[j].Package
	})
}

// Dump writes the catalog products to a JSON file.
func (c *Catalog) Dump(path string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(c)
}

// ParseTimestamp parses an ISO8601 timestamp as used by the PLCC API (e.g. "2007-06-01T00:00:00.000Z").
func ParseTimestamp(s string) (time.Time, error) {
	if s == "N/A" || s == "" {
		return time.Time{}, fmt.Errorf("timestamp is %q (unset)", s)
	}
	t, err := time.Parse("2006-01-02T15:04:05.000Z", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid ISO8601 timestamp %q: %w", s, err)
	}
	return t, nil
}

// FormatDate formats a time value as "YYYY-MM-DD".
func FormatDate(t time.Time) string {
	return t.Format("2006-01-02")
}
