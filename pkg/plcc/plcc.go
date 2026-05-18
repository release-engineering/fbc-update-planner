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
	"regexp"
	"sort"
	"time"
)

// APIURL is the Red Hat Product Life Cycle API endpoint.
const APIURL = "https://access.redhat.com/product-life-cycles/api/v2/products"

// MajorMinorRegex matches version strings in MAJOR.MINOR format (e.g. "4.12").
var MajorMinorRegex = regexp.MustCompile(`^\d+\.\d+$`)

// Catalog holds the product lifecycle data returned by the PLCC API.
type Catalog struct {
	Data []Product `json:"data"`
}

// Product represents a software product with its lifecycle versions.
type Product struct {
	Name     string    `json:"name"`
	Package  string    `json:"package"`
	Versions []Version `json:"versions"`
}

// Version represents a product version with its lifecycle phases and platform compatibility.
type Version struct {
	Name                   string  `json:"name"`
	Phases                 []Phase `json:"phases"`
	OpenShiftCompatibility string  `json:"openshift_compatibility"`
}

// Phase represents a lifecycle phase with start and end dates (ISO8601 timestamps).
type Phase struct {
	Name      string `json:"name"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

// Fetch retrieves the product catalog from the PLCC API.
func Fetch() (*Catalog, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(APIURL)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
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
	filtered := c.Data[:0]
	for _, p := range c.Data {
		if p.Package != "" {
			filtered = append(filtered, p)
		}
	}
	c.Data = filtered
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
func (c *Catalog) Dump(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

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
