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

// Package plcc provides a client for the Red Hat Product Life Cycle Center (PLCC) API.
//
// It fetches operator lifecycle data (products, versions, phases) and provides
// types for working with that data. Use [Fetch] with the default API endpoint
// or [FetchFrom] with a custom URL and HTTP client. Use [Load] to read from
// a local JSON file.
package plcc
