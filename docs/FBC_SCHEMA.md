# FBC Lifecycle Schema: `io.openshift.operators.lifecycles.v1alpha1`

This document defines the File-Based Catalog (FBC) extension schema for operator lifecycle and compatibility metadata.

## Schema Overview

Each FBC lifecycle blob describes one operator package: its supported versions, the lifecycle tier and phases for each version, and the platforms each version is compatible with.

```yaml
schema: io.openshift.operators.lifecycles.v1alpha1
package: <string>
versions:
  - name: <string>
    phases:
      - name: <string>
        startDate: <string>
        endDate: <string>
    platformCompatibility:
      - name: <string>
        versions:
          - <string>
```

## Root Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `schema` | string | yes | Always `io.openshift.operators.lifecycles.v1alpha1`. Identifies this blob type within FBC. |
| `package` | string | yes | The OLM catalog package name (e.g., `aws-efs-csi-driver-operator`). Must match the operator's package in the catalog. |
| `versions` | array | yes | List of version entries. Each entry describes one minor release of the operator. |

## Version Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Version identifier in `MAJOR.MINOR` format (e.g., `4.12`, `1.5`). |
| `phases` | array | yes | Ordered list of lifecycle phases for this version. Contiguity is validated at the PLCC input layer (REQ-DATE-04). |
| `platformCompatibility` | array | no | Platforms this version is compatible with. Omitted if no compatibility data is available. |

## Phase Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Phase name (e.g., `Full support`, `Maintenance support`). |
| `startDate` | string | yes | Start date in `YYYY-MM-DD` format. |
| `endDate` | string | yes | End date in `YYYY-MM-DD` format. Must be strictly after `startDate`. |

### Phase Continuity

Phases within a version are ordered chronologically. Contiguity (each phase starts exactly one day after the previous phase ends) is validated at the PLCC input layer by `ValidateDatesContiguity` (REQ-DATE-04) and can be enforced with the `--strict` flag.
Additionally, the FBC output cleanup pipeline removes point-in-time phases (missing start or end date) via `FilterIncompletePhases`.

## Platform Compatibility Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Platform identifier (e.g., `openshift`). |
| `versions` | array | yes | List of platform versions this operator version is compatible with, each in `MAJOR.MINOR` format. |

The `platformCompatibility` structure is designed to support multiple platforms. Currently, only `openshift` is populated from PLCC data.

## Example

```yaml
package: aws-efs-csi-driver-operator
schema: io.openshift.operators.lifecycles.v1alpha1
versions:
- name: "4.12"
  phases:
  - endDate: "2023-08-17"
    name: Full support
    startDate: "2023-01-17"
  - endDate: "2024-07-17"
    name: Maintenance support
    startDate: "2023-08-18"
  - endDate: "2025-01-17"
    name: Extended update support
    startDate: "2024-07-18"
  - endDate: "2026-01-17"
    name: Extended update support Term 2
    startDate: "2025-01-18"
  - endDate: "2027-01-17"
    name: Extended update support Term 3
    startDate: "2026-01-18"
  platformCompatibility:
  - name: openshift
    versions:
    - "4.12"
- name: "4.17"
  phases:
  - endDate: "2025-05-25"
    name: Full support
    startDate: "2024-10-01"
  - endDate: "2026-04-01"
    name: Maintenance support
    startDate: "2025-05-26"
  platformCompatibility:
  - name: openshift
    versions:
    - "4.17"
```

## Data Source

Lifecycle metadata originates from the Red Hat Product Life Cycle Center (PLCC) API. The `plcc2fbc` tool fetches, validates, and converts PLCC data into this schema. See [Validation Rules](VALIDATION_RULES.md) for entry-dropping criteria.

## Conversion Examples

- [PLCC input example](../schema-examples/plcc-schema-example.yaml) — a real PLCC entry (AWS EFS CSI driver operator)
- [FBC output example](../schema-examples/fbc-schema-example.yaml) — the same entry converted to FBC, with all unmapped fields annotated
