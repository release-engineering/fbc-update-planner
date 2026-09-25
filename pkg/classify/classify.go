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

// Package classify compares PLCC lifecycle data with OCP catalog contents
// (lifecycle entries and shipped bundles) to determine what action each
// operator needs: a PLCC data fix, an operator rebuild, or nothing.
package classify

import (
	"sort"
	"strconv"
	"strings"

	"github.com/release-engineering/fbc-update-planner/pkg/fbc"
	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

// Action is the primary call for an operator or a missing version.
// The string values are serialized into classification.json and consumed by
// external tools (plcc-check.sh jq filters, Slack payloads). Treat them as
// a stable external contract — renaming is a cross-language breaking change.
type Action string

const (
	// ActionOK means the operator has full lifecycle coverage.
	ActionOK Action = "OK"
	// ActionPLCCMissing means the product or version is absent from PLCC.
	ActionPLCCMissing Action = "PLCC missing"
	// ActionFixPLCC means PLCC data is present but cannot produce valid FBC.
	ActionFixPLCC Action = "Fix PLCC"
	// ActionNeedsRebuild means valid PLCC data exists but is missing from the catalog.
	ActionNeedsRebuild Action = "Needs rebuild"
	// ActionNoCatalogBundles means no bundles are shipped for this package.
	ActionNoCatalogBundles Action = "No catalog bundles"
)

// actionPriority returns the priority for the primary-call ordering.
// Lower values take precedence.
func actionPriority(a Action) int {
	switch a {
	case ActionFixPLCC:
		return 0
	case ActionPLCCMissing:
		return 1
	case ActionNoCatalogBundles:
		return 2
	case ActionNeedsRebuild:
		return 3
	case ActionOK:
		return 4
	default:
		return 5
	}
}

// PLCCStatus describes the PLCC data quality state for a package.
type PLCCStatus string

const (
	// PLCCStatusOK means PLCC data is present and valid.
	PLCCStatusOK PLCCStatus = "OK"
	// PLCCStatusMissing means the product is absent from PLCC data.
	PLCCStatusMissing PLCCStatus = "MISSING"
	// PLCCStatusInvalid means PLCC data is present but fails validation.
	PLCCStatusInvalid PLCCStatus = "INVALID"
	// PLCCStatusDuplicate means the package name appears in multiple PLCC products.
	PLCCStatusDuplicate PLCCStatus = "DUPLICATE"
)

// VersionGap describes a single MAJOR.MINOR version that is missing from
// the catalog lifecycle entry, along with its classification.
type VersionGap struct {
	Version string   `json:"version"`
	Action  Action   `json:"action"`
	Reasons []string `json:"reasons,omitempty"`
}

// CatalogStatus describes the catalog coverage state for a package.
// Three constant values cover the stable states; partial coverage uses a
// dynamically formatted "X/Y" string (e.g. "3/5") that is not enumerable
// at compile time.
type CatalogStatus string

const (
	// CatalogStatusOK means all bundle versions have lifecycle entries.
	CatalogStatusOK CatalogStatus = "OK"
	// CatalogStatusMissing means bundles exist but no lifecycle entry.
	CatalogStatusMissing CatalogStatus = "MISSING"
	// CatalogStatusNA means no bundles are shipped for this package.
	CatalogStatusNA CatalogStatus = "N/A"
)

// OperatorReport holds the classification for one operator package.
type OperatorReport struct {
	Package       string        `json:"package"`
	PrimaryAction Action        `json:"primaryAction"`
	PLCC          PLCCStatus    `json:"plccStatus"`
	CatalogStatus CatalogStatus `json:"catalogStatus"`
	Gaps          []VersionGap  `json:"gaps,omitempty"`
}

// CatalogData holds pre-extracted catalog information: which packages
// have lifecycle entries and bundles, and what versions each contains.
type CatalogData struct {
	// LifecycleVersions maps package name → set of MAJOR.MINOR version strings
	// present in lifecycle entries.
	LifecycleVersions map[string]map[string]bool
	// BundleVersions maps package name → set of MAJOR.MINOR version strings
	// present in shipped bundles.
	BundleVersions map[string]map[string]bool
}

// Input holds everything needed to classify a set of operators.
type Input struct {
	// Catalog is the loaded PLCC catalog (before filtering/validation).
	Catalog *plcc.Catalog
	// CatalogData holds lifecycle and bundle version sets from the OCP catalog.
	CatalogData *CatalogData
	// Packages to assess. If empty, all packages with bundles or PLCC data
	// are assessed.
	Packages []string
	// Validators are per-product PLCC validators to apply.
	Validators []plcc.Validator
	// CatalogValidators are cross-product PLCC validators (e.g.
	// ValidateNoDuplicates). When omitted, duplicate-package detection is
	// skipped and PLCCStatusDuplicate will never be returned — duplicates
	// are silently resolved by first-match-wins in buildPLCCIndex.
	CatalogValidators []plcc.CatalogValidator
}

// Classify runs the comparison and returns an OperatorReport per package,
// sorted alphabetically. The classification respects the validators selected
// for the run (typically via --validators).
func Classify(input Input) []OperatorReport {
	if input.CatalogData == nil {
		return nil
	}
	if input.Catalog == nil {
		return nil
	}

	// Build a lookup from package name to expanded PLCC product.
	plccByPackage := buildPLCCIndex(input.Catalog)

	// Determine the catalog-level rejections.
	catalogRejections := make(plcc.CatalogRejections)
	if len(input.CatalogValidators) > 0 {
		// Run catalog validators on all products (not just the requested set)
		// to catch cross-product issues like duplicate package names.
		for _, v := range input.CatalogValidators {
			for pkg, reasons := range v(input.Catalog.Data) {
				catalogRejections[pkg] = append(catalogRejections[pkg], reasons...)
			}
		}
	}

	// Determine the set of packages to assess.
	packages := input.Packages
	if len(packages) == 0 {
		packages = allPackages(plccByPackage, input.CatalogData)
	}

	reports := make([]OperatorReport, 0, len(packages))
	for _, pkg := range packages {
		r := classifyPackage(pkg, plccByPackage, input.CatalogData, catalogRejections, input.Validators)
		reports = append(reports, r)
	}

	sort.Slice(reports, func(i, j int) bool {
		return reports[i].Package < reports[j].Package
	})
	return reports
}

// buildPLCCIndex expands comma-separated package names and builds a map
// from each individual package name to its PLCC product (with version data).
func buildPLCCIndex(catalog *plcc.Catalog) map[string]*plcc.Product {
	index := make(map[string]*plcc.Product)
	for i := range catalog.Data {
		p := &catalog.Data[i]
		for _, pkg := range p.Packages() {
			// First match wins (duplicates are handled by catalog validators).
			if _, exists := index[pkg]; !exists {
				index[pkg] = p
			}
		}
	}
	return index
}

// allPackages returns the union of packages with PLCC data and packages
// with catalog bundles, sorted alphabetically.
func allPackages(plccIndex map[string]*plcc.Product, cd *CatalogData) []string {
	seen := make(map[string]bool)
	for pkg := range plccIndex {
		seen[pkg] = true
	}
	for pkg := range cd.BundleVersions {
		seen[pkg] = true
	}
	for pkg := range cd.LifecycleVersions {
		seen[pkg] = true
	}
	result := make([]string, 0, len(seen))
	for pkg := range seen {
		result = append(result, pkg)
	}
	sort.Strings(result)
	return result
}

// classifyPackage determines the status and gaps for a single package.
func classifyPackage(
	pkg string,
	plccIndex map[string]*plcc.Product,
	cd *CatalogData,
	catalogRejections plcc.CatalogRejections,
	validators []plcc.Validator,
) OperatorReport {
	r := OperatorReport{Package: pkg}

	product := plccIndex[pkg]
	bundleVersions := cd.BundleVersions[pkg]
	lifecycleVersions := cd.LifecycleVersions[pkg]

	// Compute per-product validation once. Both PLCC status and gap
	// classification consume this result, avoiding redundant calls to
	// plcc.ValidateProduct.
	var productReasons []string
	if product != nil && len(validators) > 0 {
		if _, ok := catalogRejections[pkg]; !ok {
			productReasons = plcc.ValidateProduct(*product, validators...)
		}
	}

	// Compute PLCC status using the cached validation result.
	r.PLCC = computePLCCStatus(pkg, product, catalogRejections, productReasons)

	// Compute catalog status.
	r.CatalogStatus = computeCatalogStatus(lifecycleVersions, bundleVersions)

	// No bundles shipped: the primary action depends on PLCC status.
	if len(bundleVersions) == 0 {
		if r.PLCC == PLCCStatusOK {
			r.PrimaryAction = ActionNoCatalogBundles
		} else if r.PLCC == PLCCStatusMissing {
			r.PrimaryAction = ActionPLCCMissing
		} else {
			// PLCC data has issues — Fix PLCC takes priority.
			r.PrimaryAction = ActionFixPLCC
		}
		return r
	}

	// Find missing versions: bundle versions not in lifecycle.
	missingVersions := findMissingVersions(bundleVersions, lifecycleVersions)

	if len(missingVersions) == 0 {
		r.PrimaryAction = ActionOK
		return r
	}

	// Classify each missing version using cached validation results.
	r.Gaps = classifyVersionGaps(pkg, missingVersions, product, catalogRejections, productReasons)

	// Determine primary action: highest priority among all gaps.
	r.PrimaryAction = primaryAction(r)

	return r
}

// computePLCCStatus returns the PLCC status for the package.
// productReasons are the pre-computed per-product validation results
// (computed once in classifyPackage and shared with gap classification).
func computePLCCStatus(
	pkg string,
	product *plcc.Product,
	catalogRejections plcc.CatalogRejections,
	productReasons []string,
) PLCCStatus {
	if product == nil {
		return PLCCStatusMissing
	}
	if reasons, ok := catalogRejections[pkg]; ok && len(reasons) > 0 {
		// Check if it's a duplicate.
		for _, r := range reasons {
			if strings.HasPrefix(r, plcc.LabelNoDuplicates) {
				return PLCCStatusDuplicate
			}
		}
		return PLCCStatusInvalid
	}
	if len(productReasons) > 0 {
		return PLCCStatusInvalid
	}
	return PLCCStatusOK
}

// computeCatalogStatus returns the catalog coverage state.
// Returns CatalogStatusNA when bundle data is absent, CatalogStatusMissing
// when bundles exist but no lifecycle entry, CatalogStatusOK for full
// coverage, or a dynamic "X/Y" CatalogStatus for partial coverage.
func computeCatalogStatus(lifecycleVersions, bundleVersions map[string]bool) CatalogStatus {
	if len(bundleVersions) == 0 {
		return CatalogStatusNA
	}
	if len(lifecycleVersions) == 0 {
		return CatalogStatusMissing
	}
	covered := 0
	for v := range bundleVersions {
		if lifecycleVersions[v] {
			covered++
		}
	}
	total := len(bundleVersions)
	if covered == total {
		return CatalogStatusOK
	}
	return formatCoverage(covered, total)
}

// formatCoverage returns a "covered/total" ratio string as a CatalogStatus.
func formatCoverage(covered, total int) CatalogStatus {
	return CatalogStatus(strconv.Itoa(covered) + "/" + strconv.Itoa(total))
}

// findMissingVersions returns bundle versions not covered by lifecycle, sorted.
func findMissingVersions(bundleVersions, lifecycleVersions map[string]bool) []string {
	var missing []string
	for v := range bundleVersions {
		if !lifecycleVersions[v] {
			missing = append(missing, v)
		}
	}
	sort.Slice(missing, func(i, j int) bool {
		return versionLess(missing[i], missing[j])
	})
	return missing
}

// versionLess compares MAJOR.MINOR version strings numerically.
func versionLess(a, b string) bool {
	aMaj, aMin := parseVersionParts(a)
	bMaj, bMin := parseVersionParts(b)
	if aMaj != bMaj {
		return aMaj < bMaj
	}
	return aMin < bMin
}

// parseVersionParts splits a "MAJOR.MINOR" string into its integer components.
// Returns (0, 0) if the string is not in the expected format.
func parseVersionParts(s string) (int, int) {
	parts := strings.SplitN(s, ".", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	maj, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0
	}
	min, err := strconv.Atoi(parts[1])
	if err != nil {
		return maj, 0
	}
	return maj, min
}

// classifyVersionGaps classifies each missing version.
func classifyVersionGaps(
	pkg string,
	missingVersions []string,
	product *plcc.Product,
	catalogRejections plcc.CatalogRejections,
	productReasons []string,
) []VersionGap {
	gaps := make([]VersionGap, 0, len(missingVersions))
	for _, ver := range missingVersions {
		gap := classifyOneVersion(pkg, ver, product, catalogRejections, productReasons)
		gaps = append(gaps, gap)
	}
	return gaps
}

// classifyOneVersion determines the action for a single missing version.
//
// Check hierarchy (order matters):
//  1. No product → PLCC missing
//  2. Catalog rejections (e.g. duplicate) → Fix PLCC, regardless of version
//  3. Version absent from indexed product → PLCC missing
//  4. Product-level validation failure → Fix PLCC
//  5. FBC translation failure → Fix PLCC
//  6. Otherwise → Needs rebuild
//
// Catalog rejections must precede the version-existence check because
// buildPLCCIndex resolves duplicates to the first-match product. A version
// that exists only in the non-indexed duplicate would otherwise be
// misclassified as "PLCC missing" instead of "Fix PLCC".
func classifyOneVersion(
	pkg, version string,
	product *plcc.Product,
	catalogRejections plcc.CatalogRejections,
	productReasons []string,
) VersionGap {
	// If PLCC product is missing entirely, all versions are PLCC missing.
	if product == nil {
		return VersionGap{Version: version, Action: ActionPLCCMissing}
	}

	// Check for catalog-level rejection (e.g., duplicate package name).
	// This must precede version-existence checks: when a package appears in
	// multiple PLCC products, the index resolves to only one. A version
	// absent from the indexed product but present in a duplicate would be
	// wrongly classified as "PLCC missing" if we checked existence first.
	if reasons, ok := catalogRejections[pkg]; ok && len(reasons) > 0 {
		return VersionGap{Version: version, Action: ActionFixPLCC, Reasons: reasons}
	}

	// Check if the specific version exists in PLCC data.
	var plccVersion *plcc.Version
	for i := range product.Versions {
		if product.Versions[i].Name == version {
			plccVersion = &product.Versions[i]
			break
		}
	}

	if plccVersion == nil {
		return VersionGap{Version: version, Action: ActionPLCCMissing}
	}

	// Check cached per-product validation results.
	if len(productReasons) > 0 {
		return VersionGap{Version: version, Action: ActionFixPLCC, Reasons: productReasons}
	}

	// The version exists in valid PLCC data. Try FBC translation.
	// Build a single-version product to test translation.
	testProduct := plcc.Product{
		Name:           product.Name,
		Package:        pkg,
		Versions:       []plcc.Version{*plccVersion},
		ReleaseCadence: product.ReleaseCadence,
		IsOperator:     product.IsOperator,
	}
	_, failure := fbc.TranslateProduct(testProduct, fbc.DefaultFilters()...)
	if failure != nil {
		return VersionGap{Version: version, Action: ActionFixPLCC, Reasons: failure.Reasons}
	}

	// Valid and translatable but missing from catalog → needs rebuild.
	return VersionGap{Version: version, Action: ActionNeedsRebuild}
}

// primaryAction determines the primary call for an operator report,
// using the stated priority: Fix PLCC > PLCC missing > No catalog bundles > Needs rebuild > OK.
func primaryAction(r OperatorReport) Action {
	if len(r.Gaps) == 0 {
		return ActionOK
	}

	best := ActionOK
	for _, g := range r.Gaps {
		if actionPriority(g.Action) < actionPriority(best) {
			best = g.Action
		}
	}
	return best
}
