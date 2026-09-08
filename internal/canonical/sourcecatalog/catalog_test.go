package sourcecatalog

import (
	"testing"

	"github.com/DoplexLabs/belay-engine/internal/canonical/model"
)

func TestGuardrailsMappingAdmissionIsExact(t *testing.T) {
	code := GuardrailsSourceSignalCode
	base := model.IssueSummary{
		Origin:             "numbat",
		TitleCode:          "issue.numbat_finding",
		Category:           "numbat_finding",
		SourceSignalCode:   &code,
		FingerprintVersion: "1",
	}
	mapping, ok := MatchSummary(base)
	if !ok {
		t.Fatal("reviewed mapping was not admitted")
	}
	if mapping.Key != GuardrailsConfigurationMappingKey ||
		!mapping.AcceptsRawRuleVersion("1.1") ||
		mapping.AcceptsRawRuleVersion("1.2") ||
		OpaqueRuleVersion("1.1") != "rule-b05e244762b1" {
		t.Fatalf("mapping = %+v", mapping)
	}

	tests := []model.IssueSummary{
		func() model.IssueSummary { value := base; value.Origin = "belay"; return value }(),
		func() model.IssueSummary { value := base; value.TitleCode = "issue.other"; return value }(),
		func() model.IssueSummary { value := base; value.Category = "other"; return value }(),
		func() model.IssueSummary {
			value := base
			other := "tamper.other"
			value.SourceSignalCode = &other
			return value
		}(),
		func() model.IssueSummary {
			value := base
			value.SourceSignalCode = nil
			return value
		}(),
		func() model.IssueSummary {
			value := base
			value.FingerprintVersion = "2"
			return value
		}(),
	}
	for index, value := range tests {
		if _, ok := MatchSummary(value); ok {
			t.Fatalf("incompatible summary %d was admitted: %+v", index, value)
		}
	}
}

func TestMappingsReturnsDefensiveCopies(t *testing.T) {
	first := Mappings()
	first[0].RawRuleVersions[0] = "PRIVATE_MUTATION"
	second := Mappings()
	if second[0].RawRuleVersions[0] != "1.1" {
		t.Fatalf("catalog mutation escaped: %+v", second[0])
	}
}
