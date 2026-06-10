// Package plcc provides a client for the Red Hat Product Life Cycle Center (PLCC) API.
//
// It fetches operator lifecycle data (products, versions, phases) and provides
// types for working with that data. Use [Fetch] with the default API endpoint
// or [FetchFrom] with a custom URL and HTTP client. Use [Load] to read from
// a local JSON file.
package plcc
