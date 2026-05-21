// Package fbc translates PLCC product lifecycle data into File-Based Catalog (FBC)
// entries using the io.openshift.operators.lifecycles.v1alpha1 schema.
//
// The package follows a two-phase design: lenient parsing followed by strict validation.
// newPackage translates a PLCC product without failing on bad data (unparseable
// timestamps become empty strings). [TranslateAndValidate] then runs a configurable
// filter pipeline that validates and rejects invalid entries with detailed reasons.
//
// Library callers should use [TranslateAndValidate] to get structured results ([]*Package
// and []ValidationResult). [MarshalPackages] serializes packages to JSON or YAML.
// [GenerateFBC] is a convenience wrapper that combines both steps with I/O handling.
package fbc
