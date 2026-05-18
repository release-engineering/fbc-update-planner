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

package main

import (
	"flag"
	"log"
	"os"

	"github.com/release-engineering/fbc-update-planner/pkg/fbc"
	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

const packageNotFound = 1

func main() {
	var outputPath string
	var plccDumpPath string
	var inputPath string

	flag.StringVar(&outputPath, "o", "", "write FBC data to a file instead of stdout")
	flag.StringVar(&inputPath, "i", "", "read PLCC JSON input from a file instead of fetching from API")
	flag.StringVar(&plccDumpPath, "dump-plcc", "", "dump filtered PLCC JSON to a file")
	flag.Parse()

	output := os.Stdout
	if outputPath != "" {
		f, err := os.Create(outputPath)
		if err != nil {
			log.Fatalf("failed to create output file: %v", err)
		}
		defer f.Close()
		output = f
	}

	var catalog *plcc.Catalog
	var err error
	if inputPath != "" {
		catalog, err = plcc.Load(inputPath)
	} else {
		catalog, err = plcc.Fetch()
	}
	if err != nil {
		log.Fatalf("failed to load PLCC data: %v", err)
	}

	log.Printf("fetched %d products from PLCC", catalog.Len())
	catalog.FilterPackages()
	log.Printf("found %d distinct packages", catalog.Len())
	catalog.SortByPackage()

	if plccDumpPath != "" {
		if err := catalog.Dump(plccDumpPath); err != nil {
			log.Fatalf("failed to write PLCC dump: %v", err)
		}
		log.Printf("wrote %d PLCC entries to %s", catalog.Len(), plccDumpPath)
		return
	}

	blobCount := fbc.GenerateFBC(catalog.Data, output, os.Stderr)
	if blobCount == 0 {
		log.Print("no valid FBC data found")
		os.Exit(packageNotFound)
	}
	log.Printf("wrote %d FBC blobs", blobCount)
}
