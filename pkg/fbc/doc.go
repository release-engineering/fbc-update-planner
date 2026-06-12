// Package fbc translates PLCC product lifecycle data into File-Based Catalog (FBC)
// entries using the io.openshift.operators.lifecycles.v1alpha1 schema.
//
// The package follows a two-phase design: lenient parsing followed by output cleanup.
// newPackage translates a PLCC product without failing on bad data (unparseable
// timestamps become empty strings). [Translate] then runs a configurable filter
// pipeline that cleans the output (e.g., dropping incomplete phases).
//
// Library callers should use [Translate] to get structured results ([]*Package
// and []report.ValidationResult). A [PackageWriter] serializes packages to a given format;
// use [NewPackageWriter] to obtain one. [GenerateFBC] is a convenience wrapper
// that combines both steps with I/O handling.
package fbc
