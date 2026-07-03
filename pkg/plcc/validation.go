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
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// PLCC phase name constants matching the values used in the PLCC API.
const (
	PhaseGA          = "General availability"
	PhaseFullSupport = "Full support"
	PhaseMaintenance = "Maintenance support"
	PhaseEUSTerm1    = "Extended update support"
	PhaseEUSTerm2    = "Extended update support Term 2"
	PhaseEUSTerm3    = "Extended update support Term 3"
	PhaseEndOfLife   = "End of Life"
)

// PLCC lifecycle tier values.
const (
	TierAligned  = "Aligned"
	TierAgnostic = "Agnostic"
	TierRolling  = "Rolling"
)

// TierModelCutoffDate is the OCP 4.14 GA date (2023-10-31). The three-tier
// lifecycle model (Aligned/Agnostic/Rolling) commenced with this release.
// Versions whose earliest phase predates this are exempt from tier validation.
var TierModelCutoffDate = time.Date(2023, 10, 31, 0, 0, 0, 0, time.UTC)

var (
	eusPhases       = []string{PhaseEUSTerm1, PhaseEUSTerm2, PhaseEUSTerm3}
	majorMinorRegex = regexp.MustCompile(`^\d+\.\d+$`)
)

// Validator checks a raw PLCC Product for data quality issues.
// Returns a list of warning/error strings, or nil if the product passes.
type Validator func(Product) []string

// CatalogRejections maps package names to their rejection reasons.
// By default, packages present in this map are filtered out.
type CatalogRejections map[string][]string

// CatalogValidator checks across all PLCC Products for cross-product issues.
type CatalogValidator func([]Product) CatalogRejections

// SyntaxValidators returns validators that check data format and structure.
func SyntaxValidators() []Validator {
	return validatorsByGroup("syntax")
}

// SemanticValidators returns validators that check business/lifecycle rules.
func SemanticValidators() []Validator {
	return validatorsByGroup("semantic")
}

// DefaultValidators returns the standard set of per-product validators:
// syntax validators first, then semantic validators.
func DefaultValidators() []Validator {
	return append(SyntaxValidators(), SemanticValidators()...)
}

func validatorsByGroup(group string) []Validator {
	var result []Validator
	for _, entry := range validatorRegistry {
		if entry.Group == group {
			result = append(result, entry.Validators...)
		}
	}
	return result
}

// DefaultCatalogValidators returns the standard set of catalog-level validators.
func DefaultCatalogValidators() []CatalogValidator {
	return catalogValidatorsByGroup("catalog")
}

func catalogValidatorsByGroup(group string) []CatalogValidator {
	var result []CatalogValidator
	for _, entry := range catalogValidatorRegistry {
		if entry.Group == group {
			result = append(result, entry.Validators...)
		}
	}
	return result
}

type validatorEntry struct {
	Label      string
	Group      string // "syntax" or "semantic"
	Validators []Validator
}

type catalogValidatorEntry struct {
	Label      string
	Group      string // "catalog"
	Validators []CatalogValidator
}

// validatorRegistry is the single source of truth for all validator labels,
// their group, and their implementing functions. Order is preserved.
var validatorRegistry = []validatorEntry{
	{"REQ-DATE-02", "syntax", []Validator{ValidateDatesStatic}},
	{"REQ-DATE-03", "syntax", []Validator{ValidateDatesClean}},
	{"REQ-DATE-04", "syntax", []Validator{ValidateDatesContiguity}},
	{"REQ-VER-01", "syntax", []Validator{ValidateVersionNames}},
	{"REQ-TIER-PA-01", "semantic", []Validator{ValidatePlatformAlignedPhases}},
	{"REQ-TIER-PA-02", "semantic", []Validator{ValidatePlatformAlignedOCP}},
	{"REQ-TIER-AG-01", "semantic", []Validator{ValidatePlatformAgnosticPhases}},
	{"REQ-TIER-AG-03", "semantic", []Validator{ValidatePlatformAgnosticEUSPhases}},
	{"REQ-TIER-AG-04", "semantic", []Validator{ValidatePlatformAgnosticEUSOCP}},
	{"REQ-TIER-RS-01", "semantic", []Validator{ValidateRollingStreamPhases}},
	{"REQ-TIER-RS-02", "semantic", []Validator{ValidateRollingStreamForbiddenPhases}},
	{"REQ-TIER-ALL-01", "semantic", []Validator{ValidateReleaseCadence}},
	{"REQ-TIER-ALL-02", "semantic", []Validator{ValidateTierSelected}},
	{"REQ-FIELD-02", "syntax", []Validator{ValidateOCPFormat}},
	{"CUSTOM-01", "syntax", []Validator{ValidateIsOperator}},
	{"CUSTOM-02", "syntax", []Validator{ValidateHasVersions}},
	{"CUSTOM-03", "syntax", []Validator{ValidatePhaseEndAfterStart}},
	{"CUSTOM-04", "syntax", []Validator{ValidateOCPFormatAll}},
}

var catalogValidatorRegistry = []catalogValidatorEntry{
	{"REQ-VAL-01", "catalog", []CatalogValidator{ValidateNoDuplicates}},
}

// LookupValidators resolves a list of label or group names into per-product
// and catalog validators.
// Accepted group names: "all", "syntax", "semantic", "catalog".
// Accepted labels: any label in either registry (e.g. "REQ-DATE-03", "REQ-VAL-01").
// Returns an error if any name is unknown.
func LookupValidators(names ...string) ([]Validator, []CatalogValidator, error) {
	labels := make(map[string]bool)
	for _, name := range names {
		switch name {
		case "all":
			for _, e := range validatorRegistry {
				labels[e.Label] = true
			}
			for _, e := range catalogValidatorRegistry {
				labels[e.Label] = true
			}
		case "syntax", "semantic":
			for _, e := range validatorRegistry {
				if e.Group == name {
					labels[e.Label] = true
				}
			}
		case "catalog":
			for _, e := range catalogValidatorRegistry {
				if e.Group == name {
					labels[e.Label] = true
				}
			}
		default:
			found := false
			for _, e := range validatorRegistry {
				if e.Label == name {
					found = true
					break
				}
			}
			if !found {
				for _, e := range catalogValidatorRegistry {
					if e.Label == name {
						found = true
						break
					}
				}
			}
			if !found {
				return nil, nil, fmt.Errorf("unknown validator %q", name)
			}
			labels[name] = true
		}
	}
	var prodResult []Validator
	for _, e := range validatorRegistry {
		if labels[e.Label] {
			prodResult = append(prodResult, e.Validators...)
		}
	}
	var catResult []CatalogValidator
	for _, e := range catalogValidatorRegistry {
		if labels[e.Label] {
			catResult = append(catResult, e.Validators...)
		}
	}
	return prodResult, catResult, nil
}

// ListValidators returns a formatted string listing available validator
// groups and labels.
func ListValidators() string {
	var b strings.Builder
	b.WriteString("Groups:\n")
	b.WriteString("  all        all validators (syntax + semantic + catalog)\n")
	b.WriteString("  syntax     data format and structure checks\n")
	b.WriteString("  semantic   business/lifecycle rule checks\n")
	b.WriteString("  catalog    cross-product catalog checks\n")
	b.WriteString("\nLabels:\n")
	for _, entry := range validatorRegistry {
		fmt.Fprintf(&b, "  %-16s [%s]\n", entry.Label, entry.Group)
	}
	for _, entry := range catalogValidatorRegistry {
		fmt.Fprintf(&b, "  %-16s [%s]\n", entry.Label, entry.Group)
	}
	return b.String()
}

// ValidateProduct runs all provided validators on a single product and returns
// the combined list of reasons. Returns nil if all validators pass.
func ValidateProduct(p Product, validators ...Validator) []string {
	var reasons []string
	for _, v := range validators {
		reasons = append(reasons, v(p)...)
	}
	return reasons
}

// Validate runs catalog validators across the catalog's products and returns
// per-package reasons. If no validators are provided, uses
// DefaultCatalogValidators(). When strict is true, products that trigger catalog
// warnings (e.g. duplicated package names) are removed from c.Data.
func (c *Catalog) Validate(strict bool, validators ...CatalogValidator) CatalogRejections {
	if len(validators) == 0 {
		validators = DefaultCatalogValidators()
	}
	rejections := make(CatalogRejections)
	for _, v := range validators {
		for pkg, r := range v(c.Data) {
			rejections[pkg] = append(rejections[pkg], r...)
		}
	}
	if strict && len(rejections) > 0 {
		var filtered []Product
		for _, p := range c.Data {
			if _, rejected := rejections[p.Package]; !rejected {
				filtered = append(filtered, p)
			}
		}
		c.Data = filtered
	}
	return rejections
}

// ValidateDatesStatic checks that dates resolve to static values using the
// API's date format classification.
// REQ-DATE-02
func ValidateDatesStatic(p Product) []string {
	var reasons []string
	for _, v := range p.Versions {
		for _, ph := range v.Phases {
			if ph.StartDateFormat == "string" && ph.StartDate != "N/A" && ph.StartDate != "" {
				reasons = append(reasons, fmt.Sprintf("REQ-DATE-02: version %q phase %q: start date is not a static value (%s)", v.Name, ph.Name, ph.StartDate))
			}
			if ph.EndDateFormat == "string" && ph.EndDate != "N/A" && ph.EndDate != "" {
				reasons = append(reasons, fmt.Sprintf("REQ-DATE-02: version %q phase %q: end date is not a static value (%s)", v.Name, ph.Name, ph.EndDate))
			}
		}
	}
	return reasons
}

// ValidateDatesClean checks that all non-empty, non-N/A dates cleanly parse.
// REQ-DATE-03
func ValidateDatesClean(p Product) []string {
	var reasons []string
	for _, v := range p.Versions {
		for _, ph := range v.Phases {
			if ph.StartDate != "" && ph.StartDate != "N/A" && !isParseableTimestamp(ph.StartDate) {
				reasons = append(reasons, fmt.Sprintf("REQ-DATE-03: version %q phase %q: start date does not parse (%s)", v.Name, ph.Name, ph.StartDate))
			}
			if ph.EndDate != "" && ph.EndDate != "N/A" && !isParseableTimestamp(ph.EndDate) {
				reasons = append(reasons, fmt.Sprintf("REQ-DATE-03: version %q phase %q: end date does not parse (%s)", v.Name, ph.Name, ph.EndDate))
			}
		}
	}
	return reasons
}

// ValidateDatesContiguity checks that consecutive phases with parseable dates are
// contiguous: each phase starts exactly one day after the previous one ends.
// REQ-DATE-04
func ValidateDatesContiguity(p Product) []string {
	var reasons []string
	for _, v := range p.Versions {
		var validPhases []struct {
			name       string
			start, end time.Time
		}
		for _, ph := range v.Phases {
			if !isParseableTimestamp(ph.StartDate) || !isParseableTimestamp(ph.EndDate) {
				continue
			}
			start := mustParseDate(ph.StartDate)
			end := mustParseDate(ph.EndDate)
			if !start.After(end) {
				validPhases = append(validPhases, struct {
					name       string
					start, end time.Time
				}{ph.Name, start, end})
			}
		}
		for i := 1; i < len(validPhases); i++ {
			expectedStart := validPhases[i-1].end.AddDate(0, 0, 1)
			if !validPhases[i].start.Equal(expectedStart) {
				reasons = append(reasons, fmt.Sprintf("REQ-DATE-04: version %q phase %q: start (%s) is not one day after previous phase %q end (%s)",
					v.Name, validPhases[i].name, FormatDate(validPhases[i].start), validPhases[i-1].name, FormatDate(validPhases[i-1].end)))
			}
		}
	}
	return reasons
}

// ValidateVersionNames checks that all version names match MAJOR.MINOR format.
// REQ-VER-01
func ValidateVersionNames(p Product) []string {
	var reasons []string
	for _, v := range p.Versions {
		if !majorMinorRegex.MatchString(v.Name) {
			reasons = append(reasons, fmt.Sprintf("REQ-VER-01: version name %q is not MAJOR.MINOR", v.Name))
		}
	}
	return reasons
}

// ValidatePlatformAlignedPhases checks platform-aligned versions for required phases
// with parseable dates.
// REQ-TIER-PA-01
func ValidatePlatformAlignedPhases(p Product) []string {
	var reasons []string
	for _, v := range p.Versions {
		if isPreTierModel(v) || versionTier(v) != TierAligned {
			continue
		}
		required := []string{PhaseFullSupport, PhaseMaintenance, PhaseEUSTerm1, PhaseEUSTerm2, PhaseEUSTerm3}
		for _, name := range required {
			if !hasPhaseWithParseableDates(v, name) {
				reasons = append(reasons, fmt.Sprintf("REQ-TIER-PA-01: version %q: platform-aligned missing required phase %q with parseable dates", v.Name, name))
			}
		}
	}
	return reasons
}

// ValidatePlatformAlignedOCP checks platform-aligned versions have OCP compatibility specified.
// REQ-TIER-PA-02
func ValidatePlatformAlignedOCP(p Product) []string {
	var reasons []string
	for _, v := range p.Versions {
		if isPreTierModel(v) || versionTier(v) != TierAligned {
			continue
		}
		ocp := strings.TrimSpace(v.OpenShiftCompatibility)
		if ocp == "" || ocp == "N/A" {
			reasons = append(reasons, fmt.Sprintf("REQ-TIER-PA-02: version %q: platform-aligned missing OCP compatibility", v.Name))
		}
	}
	return reasons
}

// ValidatePlatformAgnosticPhases checks platform-agnostic versions for required phases
// with parseable dates.
// REQ-TIER-AG-01
func ValidatePlatformAgnosticPhases(p Product) []string {
	var reasons []string
	for _, v := range p.Versions {
		if isPreTierModel(v) || versionTier(v) != TierAgnostic {
			continue
		}
		for _, name := range []string{PhaseFullSupport, PhaseMaintenance} {
			if !hasPhaseWithParseableDates(v, name) {
				reasons = append(reasons, fmt.Sprintf("REQ-TIER-AG-01: version %q: platform-agnostic missing required phase %q with parseable dates", v.Name, name))
			}
		}
	}
	return reasons
}

// ValidatePlatformAgnosticEUSPhases checks that EUS-aligned platform-agnostic versions
// have all three EUS Term phases with parseable dates. If none have parseable dates,
// the version is not EUS-aligned and passes.
// REQ-TIER-AG-03
func ValidatePlatformAgnosticEUSPhases(p Product) []string {
	var reasons []string
	for _, v := range p.Versions {
		if isPreTierModel(v) || versionTier(v) != TierAgnostic || !isVersionEUSAligned(v) {
			continue
		}
		for _, eus := range eusPhases {
			if !hasPhaseWithParseableDates(v, eus) {
				reasons = append(reasons, fmt.Sprintf("REQ-TIER-AG-03: version %q: EUS-aligned platform-agnostic missing required EUS phase %q with parseable dates", v.Name, eus))
			}
		}
	}
	return reasons
}

// ValidatePlatformAgnosticEUSOCP checks that EUS-aligned platform-agnostic versions
// have OCP compatibility specified.
// REQ-TIER-AG-04
func ValidatePlatformAgnosticEUSOCP(p Product) []string {
	var reasons []string
	for _, v := range p.Versions {
		if isPreTierModel(v) || versionTier(v) != TierAgnostic || !isVersionEUSAligned(v) {
			continue
		}
		ocp := strings.TrimSpace(v.OpenShiftCompatibility)
		if ocp == "" || ocp == "N/A" {
			reasons = append(reasons, fmt.Sprintf("REQ-TIER-AG-04: version %q: EUS-aligned platform-agnostic missing OCP compatibility", v.Name))
		}
	}
	return reasons
}

// ValidateRollingStreamPhases checks rolling stream versions have Full Support
// with parseable dates.
// REQ-TIER-RS-01
func ValidateRollingStreamPhases(p Product) []string {
	var reasons []string
	for _, v := range p.Versions {
		if isPreTierModel(v) || versionTier(v) != TierRolling {
			continue
		}
		if !hasPhaseWithParseableDates(v, PhaseFullSupport) {
			reasons = append(reasons, fmt.Sprintf("REQ-TIER-RS-01: version %q: rolling stream missing required phase %q with parseable dates", v.Name, PhaseFullSupport))
		}
	}
	return reasons
}

// ValidateRollingStreamForbiddenPhases checks rolling stream versions don't include
// Maintenance or EUS phases.
// REQ-TIER-RS-02
func ValidateRollingStreamForbiddenPhases(p Product) []string {
	var reasons []string
	for _, v := range p.Versions {
		if isPreTierModel(v) || versionTier(v) != TierRolling {
			continue
		}
		phaseNames := phaseNameSet(v)
		if phaseNames[PhaseMaintenance] {
			reasons = append(reasons, fmt.Sprintf("REQ-TIER-RS-02: version %q: rolling stream must not include phase %q", v.Name, PhaseMaintenance))
		}
		for _, eus := range eusPhases {
			if phaseNames[eus] {
				reasons = append(reasons, fmt.Sprintf("REQ-TIER-RS-02: version %q: rolling stream must not include phase %q", v.Name, eus))
			}
		}
	}
	return reasons
}

// ValidateReleaseCadence checks that operator products have a release cadence specified.
// Non-operator products are skipped.
// REQ-TIER-ALL-01
func ValidateReleaseCadence(p Product) []string {
	if !p.IsOperator {
		return nil
	}
	cadence := strings.TrimSpace(p.ReleaseCadence)
	if cadence == "" || cadence == "Not Specified" {
		return []string{fmt.Sprintf("REQ-TIER-ALL-01: release cadence not specified (release_cadence=%q)", p.ReleaseCadence)}
	}
	return nil
}

// ValidateTierSelected checks that every version of an operator product has a
// lifecycle tier selected. Non-operator products are skipped.
// REQ-TIER-ALL-02
func ValidateTierSelected(p Product) []string {
	if !p.IsOperator {
		return nil
	}
	var reasons []string
	for _, v := range p.Versions {
		if isPreTierModel(v) {
			continue
		}
		tier := versionTier(v)
		if tier == "" || tier == "N/A" || tier == "-" {
			reasons = append(reasons, fmt.Sprintf("REQ-TIER-ALL-02: version %q: lifecycle tier not selected (tier=%q)", v.Name, v.Tier))
		}
	}
	return reasons
}

// ValidateOCPFormat checks that OCP compatibility values on platform-aligned versions
// match MAJOR.MINOR format.
// REQ-FIELD-02
func ValidateOCPFormat(p Product) []string {
	var reasons []string
	for _, v := range p.Versions {
		if isPreTierModel(v) || versionTier(v) != TierAligned {
			continue
		}
		ocp := strings.TrimSpace(v.OpenShiftCompatibility)
		if ocp == "" || ocp == "N/A" {
			continue
		}
		for _, part := range strings.Split(ocp, ",") {
			ver := strings.TrimSpace(part)
			if ver != "" && !majorMinorRegex.MatchString(ver) {
				reasons = append(reasons, fmt.Sprintf("REQ-FIELD-02: version %q: OCP compatibility %q is not MAJOR.MINOR", v.Name, ver))
			}
		}
	}
	return reasons
}

// ValidateNoDuplicates checks that no package name appears in multiple products.
// REQ-VAL-01
func ValidateNoDuplicates(products []Product) CatalogRejections {
	pkgCount := make(map[string]int)
	for _, p := range products {
		if p.Package != "" {
			pkgCount[p.Package]++
		}
	}
	rejections := make(CatalogRejections)
	for pkg, count := range pkgCount {
		if count > 1 {
			rejections[pkg] = []string{fmt.Sprintf("REQ-VAL-01: package %q appears in %d products", pkg, count)}
		}
	}
	return rejections
}

// ValidateIsOperator checks that the product has a package name and is flagged as an operator.
// CUSTOM-01
func ValidateIsOperator(p Product) []string {
	var reasons []string
	if p.Package == "" {
		reasons = append(reasons, "CUSTOM-01: product has no package name")
	}
	if !p.IsOperator {
		reasons = append(reasons, "CUSTOM-01: product is missing the is_operator flag")
	}
	return reasons
}

// ValidateHasVersions checks that the product has at least one version.
// CUSTOM-02
func ValidateHasVersions(p Product) []string {
	if len(p.Versions) == 0 {
		return []string{"CUSTOM-02: package has no versions"}
	}
	return nil
}

// ValidatePhaseEndAfterStart checks that phase end dates are after start dates.
// CUSTOM-03
func ValidatePhaseEndAfterStart(p Product) []string {
	var reasons []string
	for _, v := range p.Versions {
		for _, ph := range v.Phases {
			if !isParseableTimestamp(ph.StartDate) || !isParseableTimestamp(ph.EndDate) {
				continue
			}
			start := mustParseDate(ph.StartDate)
			end := mustParseDate(ph.EndDate)
			if !end.After(start) {
				reasons = append(reasons, fmt.Sprintf("CUSTOM-03: version %q phase %q: end (%s) is not after start (%s)",
					v.Name, ph.Name, FormatDate(end), FormatDate(start)))
			}
		}
	}
	return reasons
}

// ValidateOCPFormatAll checks that OCP compatibility values match MAJOR.MINOR format
// on all versions (not just platform-aligned).
// CUSTOM-04
func ValidateOCPFormatAll(p Product) []string {
	var reasons []string
	for _, v := range p.Versions {
		if isPreTierModel(v) || versionTier(v) == TierAligned {
			continue
		}
		ocp := strings.TrimSpace(v.OpenShiftCompatibility)
		if ocp == "" || ocp == "N/A" {
			continue
		}
		for _, part := range strings.Split(ocp, ",") {
			ver := strings.TrimSpace(part)
			if ver != "" && !majorMinorRegex.MatchString(ver) {
				reasons = append(reasons, fmt.Sprintf("CUSTOM-04: version %q: OCP compatibility %q is not MAJOR.MINOR", v.Name, ver))
			}
		}
	}
	return reasons
}

func isPreTierModel(v Version) bool {
	var earliest time.Time
	for _, ph := range v.Phases {
		t, err := ParseTimestamp(ph.StartDate)
		if err != nil {
			continue
		}
		if earliest.IsZero() || t.Before(earliest) {
			earliest = t
		}
	}
	return earliest.IsZero() || earliest.Before(TierModelCutoffDate)
}

func versionTier(v Version) string {
	return strings.TrimSpace(v.Tier)
}

func phaseNameSet(v Version) map[string]bool {
	names := make(map[string]bool, len(v.Phases))
	for _, ph := range v.Phases {
		names[ph.Name] = true
	}
	return names
}

func isEUSPhase(name string) bool {
	return slices.Contains(eusPhases, name)
}

func isVersionEUSAligned(v Version) bool {
	for _, ph := range v.Phases {
		if isEUSPhase(ph.Name) && hasAnyParseableDate(ph) {
			return true
		}
	}
	return false
}

func hasAnyParseableDate(ph Phase) bool {
	return isParseableTimestamp(ph.StartDate) || isParseableTimestamp(ph.EndDate)
}

func hasPhaseWithParseableDates(v Version, phaseName string) bool {
	for _, ph := range v.Phases {
		if ph.Name == phaseName && isParseableTimestamp(ph.StartDate) && isParseableTimestamp(ph.EndDate) {
			return true
		}
	}
	return false
}

func isParseableTimestamp(s string) bool {
	_, err := ParseTimestamp(s)
	return err == nil
}

func mustParseDate(s string) time.Time {
	t, _ := ParseTimestamp(s)
	return t
}
