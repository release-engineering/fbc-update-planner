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

package plcccheck

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/release-engineering/fbc-update-planner/pkg/catalog"
	"github.com/release-engineering/fbc-update-planner/pkg/fbc"
	"github.com/release-engineering/fbc-update-planner/pkg/plcc"
)

// Action is the operator-level recommendation shown alongside both statuses.
// Version and package findings are recorded separately as Issues and Failures.
type Action string

const (
	OK            Action = "OK"
	AddPLCC       Action = "PLCC add"
	FixPLCC       Action = "PLCC fix"
	AddOperator   Action = "OPERATOR add"
	BuildOperator Action = "OPERATOR build"
	Skipped       Action = "SKIPPED"
)

// PLCCStatus summarizes source availability, pipeline acceptance, and version
// completeness. Precedence is MISSING, DUPLICATE, INVALID, REGRESSED, INCOMPLETE,
// OK. Every finding is retained regardless of which status takes precedence.
type PLCCStatus string

const (
	PLCCOK         PLCCStatus = "OK"
	PLCCAbsent     PLCCStatus = "MISSING"
	PLCCInvalid    PLCCStatus = "INVALID"
	PLCCDuplicate  PLCCStatus = "DUPLICATE"
	PLCCIncomplete PLCCStatus = "INCOMPLETE"
	PLCCRegressed  PLCCStatus = "REGRESSED"
)

// FailureStage identifies which pipeline layer rejected a source product.
type FailureStage string

const (
	PLCCValidation FailureStage = "plcc-validation"
	FBCTranslation FailureStage = "fbc-translation" // Includes mandatory filters.
)

// Failure retains source identity and original messages. Validator is present
// for PLCC failures; FBC translation currently exposes reason strings only.
// Packages contains the rule's targets, which can differ from the aliases
// affected by rejection of the whole product.
type Failure struct {
	SourceIndex int                 `json:"sourceIndex"`
	Stage       FailureStage        `json:"stage"`
	Validator   *plcc.ValidatorInfo `json:"validator,omitempty"`
	Packages    []string            `json:"packages"`
	Reasons     []string            `json:"reasons"`
}

// IssueKind identifies missing content independently of the operator's action.
type IssueKind string

const (
	MissingPLCCPackage             IssueKind = "plcc-package-missing"
	MissingPLCCVersion             IssueKind = "plcc-version-missing"
	RegressedPLCCVersion           IssueKind = "plcc-version-regressed"
	MissingCatalogBundles          IssueKind = "catalog-bundles-missing"
	MissingCatalogLifecycle        IssueKind = "catalog-lifecycle-missing"
	MissingCatalogLifecycleVersion IssueKind = "catalog-lifecycle-version-missing"
)

// Issue records one missing item for the owning package. Version is nil for
// package-level issues. A regressed version exists in shipped lifecycle data
// but is absent from current PLCC, even if no bundle requires that version.
type Issue struct {
	Kind    IssueKind       `json:"kind"`
	Version *fbc.MajorMinor `json:"version,omitempty"`
}

// Coverage records catalog evidence independently of PLCC health. Version lists
// are unique and numerically sorted; Bundles retains original identities/order.
type Coverage struct {
	Bundles           []catalog.Bundle `json:"bundles"`
	HasLifecycle      bool             `json:"hasLifecycle"`
	BundleVersions    []fbc.MajorMinor `json:"bundleVersions"`
	LifecycleVersions []fbc.MajorMinor `json:"lifecycleVersions"`
	Covered           int              `json:"covered"`
}

// Status returns NO BUNDLES, MISSING, OK, or X/Y, in that precedence order.
func (c Coverage) Status() string {
	if len(c.BundleVersions) == 0 {
		return "NO BUNDLES"
	}
	if !c.HasLifecycle {
		return "MISSING"
	}
	if c.Covered == len(c.BundleVersions) {
		return "OK"
	}
	return fmt.Sprintf("%d/%d", c.Covered, len(c.BundleVersions))
}

// PackageAssessment contains the facts and actions shared by all report formats.
// SourceProducts indexes the original PLCC snapshot. SourceVersions retains raw
// names (including malformed names) from every matching source product.
// ProducedVersions contains successful whole-product translation output; a
// failure in another product for the same package still takes priority.
type PackageAssessment struct {
	Name             string           `json:"name"`
	SourceProducts   []int            `json:"sourceProducts"`
	SourceVersions   []string         `json:"sourceVersions"`
	ProducedVersions []fbc.MajorMinor `json:"producedVersions"`
	PLCC             PLCCStatus       `json:"plcc"`
	Failures         []Failure        `json:"failures"`
	Catalog          *Coverage        `json:"catalog,omitempty"`
	Issues           []Issue          `json:"issues"`
	Action           Action           `json:"action"`
	SkipReason       string           `json:"skipReason,omitempty"`
}

// Assessment is a caller-owned report model. Packages follows explicit request
// order, with duplicates removed, or alphabetical order for an all-package run.
type Assessment struct {
	Validators     []plcc.ValidatorInfo `json:"validators"`
	Packages       []PackageAssessment  `json:"packages"`
	CatalogChecked bool                 `json:"catalogChecked"`

	// Pipeline outputs are retained for artifact generation without repeating
	// validation or translation. FilteredPLCC preserves selected product shape
	// and excludes PLCC validation failures; FBC also excludes conversion and
	// filter failures. Neither is part of the JSON assessment evidence.
	FilteredPLCC *plcc.Catalog  `json:"-"`
	FBC          []*fbc.Package `json:"-"`
}

// Assess validates and translates one PLCC snapshot and checks its completeness
// against all bundle versions and all shipped lifecycle versions in the catalog.
// It does no I/O and does not mutate its inputs. Nil inventory skips catalog
// checks; a non-nil empty inventory means the catalog contains no packages.
// Options uses Dataset selection semantics, including nil versus empty Packages.
// In all-package mode the assessed set includes PLCC and catalog-only packages.
// Selected PLCC validators and all default FBC filters apply; there is no
// per-version retry or permissive translation of rejected products.
// Invalid options or unrecognizable catalog versions return no assessment.
func Assess(source *plcc.Catalog, inventory *catalog.Inventory, options plcc.DatasetOptions) (*Assessment, error) {
	dataset, err := plcc.NewDataset(source, options)
	if err != nil {
		return nil, err
	}
	validation := dataset.Validate()
	if err := dataset.FilterInvalid(); err != nil {
		return nil, err
	}
	result := &Assessment{
		Validators: dataset.Validators(), CatalogChecked: inventory != nil,
		FilteredPLCC: dataset.Catalog(),
	}
	snapshot := dataset.Source()
	packages := make(map[string]*PackageAssessment)
	for _, finding := range validation.Products {
		product := snapshot.Data[finding.SourceIndex]
		for _, name := range finding.Packages {
			pkg := packages[name]
			if pkg == nil {
				pkg = &PackageAssessment{Name: name}
				packages[name] = pkg
			}
			pkg.SourceProducts = append(pkg.SourceProducts, finding.SourceIndex)
			for _, version := range product.Versions {
				pkg.SourceVersions = append(pkg.SourceVersions, version.Name)
			}
			if len(finding.Failures) > 0 {
				for _, failure := range finding.Failures {
					pkg.Failures = append(pkg.Failures, Failure{
						SourceIndex: finding.SourceIndex, Stage: PLCCValidation,
						Validator: &failure.Validator, Packages: failure.Packages, Reasons: failure.Reasons,
					})
				}
				continue
			}
			product.Package = name
			translated, failure := fbc.TranslateProduct(product, fbc.DefaultFilters()...)
			if failure != nil {
				pkg.Failures = append(pkg.Failures, Failure{
					SourceIndex: finding.SourceIndex, Stage: FBCTranslation,
					Packages: []string{name}, Reasons: failure.Reasons,
				})
				continue
			}
			for _, version := range translated.Versions {
				pkg.ProducedVersions = append(pkg.ProducedVersions, version.Name)
			}
			result.FBC = append(result.FBC, translated)
		}
	}

	names := selectedNames(options.Packages, packages, inventory)
	result.Packages = make([]PackageAssessment, 0, len(names))
	slices.SortStableFunc(result.FBC, func(a, b *fbc.Package) int { return cmp.Compare(a.Name, b.Name) })
	for _, name := range names {
		pkg := packages[name]
		if pkg == nil {
			pkg = &PackageAssessment{Name: name}
		}
		slices.Sort(pkg.SourceProducts)
		slices.Sort(pkg.SourceVersions)
		pkg.SourceVersions = slices.Compact(pkg.SourceVersions)
		pkg.ProducedVersions = uniqueVersions(pkg.ProducedVersions)
		if inventory != nil {
			pkg.Catalog, err = coverage(name, inventory.Packages[name])
			if err != nil {
				return nil, err
			}
		}
		if err := pkg.collectIssues(); err != nil {
			return nil, err
		}
		pkg.setPLCCStatus()
		pkg.Action = pkg.primaryAction()
		result.Packages = append(result.Packages, *pkg)
	}
	return result, nil
}

func selectedNames(requested []string, packages map[string]*PackageAssessment, inventory *catalog.Inventory) []string {
	var names []string
	seen := make(map[string]bool)
	add := func(name string) {
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	if requested != nil {
		for _, name := range requested {
			add(name)
		}
		return names
	}
	for name := range packages {
		add(name)
	}
	if inventory != nil {
		for name := range inventory.Packages {
			add(name)
		}
	}
	slices.Sort(names)
	return names
}

func (p *PackageAssessment) setPLCCStatus() {
	p.PLCC = PLCCOK
	if len(p.SourceProducts) == 0 {
		p.PLCC = PLCCAbsent
		return
	}
	for _, failure := range p.Failures {
		if failure.Validator != nil && failure.Validator.Label == "REQ-VAL-01" && slices.Contains(failure.Packages, p.Name) {
			p.PLCC = PLCCDuplicate
			return
		}
	}
	switch {
	case len(p.Failures) > 0:
		p.PLCC = PLCCInvalid
	case p.hasIssue(RegressedPLCCVersion):
		p.PLCC = PLCCRegressed
	case p.hasIssue(MissingPLCCVersion):
		p.PLCC = PLCCIncomplete
	}
}

func (p PackageAssessment) hasIssue(kind IssueKind) bool {
	return slices.ContainsFunc(p.Issues, func(issue Issue) bool { return issue.Kind == kind })
}

// collectIssues retains package-level issues first, followed by version issues
// in numeric order (PLCC before catalog for the same version). Existing catalog
// coverage never suppresses a missing PLCC version or regression finding.
func (p *PackageAssessment) collectIssues() error {
	if len(p.SourceProducts) == 0 {
		p.Issues = append(p.Issues, Issue{Kind: MissingPLCCPackage})
	}
	if p.Catalog == nil {
		return nil
	}
	if len(p.Catalog.BundleVersions) == 0 {
		p.Issues = append(p.Issues, Issue{Kind: MissingCatalogBundles})
	}
	if !p.Catalog.HasLifecycle {
		p.Issues = append(p.Issues, Issue{Kind: MissingCatalogLifecycle})
	}
	versions := slices.Clone(p.Catalog.BundleVersions)
	if p.Catalog.HasLifecycle {
		versions = append(versions, p.Catalog.LifecycleVersions...)
	}
	for _, version := range uniqueVersions(versions) {
		inPLCC := slices.Contains(p.SourceVersions, version.String())
		inLifecycle := p.Catalog.HasLifecycle && slices.Contains(p.Catalog.LifecycleVersions, version)
		if !inPLCC {
			kind := MissingPLCCVersion
			if inLifecycle {
				kind = RegressedPLCCVersion
			}
			p.Issues = append(p.Issues, Issue{Kind: kind, Version: &version})
		}
		if slices.Contains(p.Catalog.BundleVersions, version) && !inLifecycle {
			if inPLCC && len(p.Failures) == 0 && !slices.Contains(p.ProducedVersions, version) {
				return fmt.Errorf("package %q: PLCC version %s was neither produced nor rejected by translation", p.Name, version)
			}
			p.Issues = append(p.Issues, Issue{Kind: MissingCatalogLifecycleVersion, Version: &version})
		}
	}
	return nil
}

func (p PackageAssessment) primaryAction() Action {
	if p.PLCC == PLCCAbsent {
		return AddPLCC
	}
	if p.PLCC != PLCCOK {
		return FixPLCC
	}
	if p.Catalog != nil && p.Catalog.Status() == "NO BUNDLES" {
		return AddOperator
	}
	if p.Catalog != nil && p.Catalog.Status() != "OK" {
		return BuildOperator
	}
	return OK
}
