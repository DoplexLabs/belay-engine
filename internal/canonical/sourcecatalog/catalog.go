package sourcecatalog

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/DoplexLabs/belay-engine/internal/canonical/model"
)

const (
	CatalogVersion = "belay.attention-families.v1"

	GuardrailsConfigurationMappingKey = "attention.agent_guardrails_configuration"
	GuardrailsSourceSignalCode        = "tamper.guardrails_off"

	GuardrailsEvidenceTitle  = "Agent guardrail configuration observed"
	GuardrailsEvidenceDetail = "This configuration event supported the safety-confirmation signal. " +
		"Belay does not retain the configuration value or body."
)

type Mapping struct {
	Key                  string
	Version              string
	GroupingVersion      string
	Origin               string
	TitleCode            string
	Category             string
	SourceSignalCode     string
	RawRuleVersions      []string
	FingerprintVersions  []string
	AttentionKind        string
	AttentionSeverity    string
	DefaultVisible       bool
	DisplayTitle         string
	ObservationStatement string
	Caveat               string
	NextEvidenceAction   string
}

var guardrailsConfiguration = Mapping{
	Key:                 GuardrailsConfigurationMappingKey,
	Version:             "1",
	GroupingVersion:     "1",
	Origin:              "numbat",
	TitleCode:           "issue.numbat_finding",
	Category:            "numbat_finding",
	SourceSignalCode:    GuardrailsSourceSignalCode,
	RawRuleVersions:     []string{"1.1"},
	FingerprintVersions: []string{"1"},
	AttentionKind:       model.AttentionKindIssue,
	AttentionSeverity:   "low",
	DefaultVisible:      true,
	DisplayTitle:        "Agent safety confirmations may be disabled",
	ObservationStatement: "A mapped upstream rule reported retained configuration evidence " +
		"associated with disabled agent safety confirmations.",
	Caveat: "This does not establish one shared configuration, project, cause, " +
		"malicious tampering, or an unsafe action.",
	NextEvidenceAction: "inspect_exact_records",
}

func Mappings() []Mapping {
	return []Mapping{cloneMapping(guardrailsConfiguration)}
}

func MappingByKey(key, version, groupingVersion string) (Mapping, bool) {
	for _, mapping := range Mappings() {
		if mapping.Key == key &&
			mapping.Version == version &&
			mapping.GroupingVersion == groupingVersion {
			return mapping, true
		}
	}
	return Mapping{}, false
}

func MatchSummary(summary model.IssueSummary) (Mapping, bool) {
	for _, mapping := range Mappings() {
		if mapping.DefaultVisible &&
			summary.Origin == mapping.Origin &&
			summary.TitleCode == mapping.TitleCode &&
			summary.Category == mapping.Category &&
			summary.SourceSignalCode != nil &&
			*summary.SourceSignalCode == mapping.SourceSignalCode &&
			contains(mapping.FingerprintVersions, summary.FingerprintVersion) {
			return mapping, true
		}
	}
	return Mapping{}, false
}

func (mapping Mapping) AcceptsRawRuleVersion(version string) bool {
	return contains(mapping.RawRuleVersions, version)
}

func OpaqueRuleVersion(version string) string {
	sum := sha256.Sum256([]byte(version))
	return "rule-" + hex.EncodeToString(sum[:6])
}

func cloneMapping(mapping Mapping) Mapping {
	mapping.RawRuleVersions = append([]string(nil), mapping.RawRuleVersions...)
	mapping.FingerprintVersions = append([]string(nil), mapping.FingerprintVersions...)
	return mapping
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
