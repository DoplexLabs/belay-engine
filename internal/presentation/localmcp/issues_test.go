package localmcp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/DoplexLabs/belay-engine/internal/presentation/readmodel"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestIssueEvidenceToolsExposeStrictSafeWorkflow(t *testing.T) {
	repository := &testRepository{}
	session := newTestClient(t, repository)

	listResult := callTool(t, session, "list_issues", map[string]any{})
	assertStrictSuccess(t, listResult)
	list := asObject(t, asObject(t, listResult.StructuredContent)["readmodel"])
	if list["schema_version"] != readmodel.SchemaVersion {
		t.Fatalf("list schema_version = %#v", list["schema_version"])
	}
	if list["projection_version"] != readmodel.IssueProjectionVersion {
		t.Fatalf("list projection_version = %#v", list["projection_version"])
	}
	viewCursor, ok := list["view_cursor"].(string)
	if !ok || viewCursor == "" {
		t.Fatalf("list view_cursor = %#v", list["view_cursor"])
	}
	selection := asObject(t, list["selection"])
	if selection["attention_kind"] != "issue" || selection["experimental"] != "stable" {
		t.Fatalf("default issue selection = %#v", selection)
	}

	detailResult := callTool(t, session, "get_issue", map[string]any{
		"issue_id":    testIssueID,
		"view_cursor": viewCursor,
	})
	assertStrictSuccess(t, detailResult)
	detail := asObject(t, asObject(t, detailResult.StructuredContent)["readmodel"])
	data := asObject(t, detail["data"])
	occurrences, ok := data["occurrences"].([]any)
	if !ok || len(occurrences) != 1 {
		t.Fatalf("occurrences = %#v", data["occurrences"])
	}
	encodedDetail, err := json.Marshal(detailResult.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"PRIVATE_ORIGIN_RECORD",
		"PRIVATE_DIMENSION",
		"analysis_generation",
		"origin_record_id",
		"dimensions",
	} {
		if strings.Contains(string(encodedDetail), forbidden) {
			t.Fatalf("issue detail exposed private field %q: %s", forbidden, encodedDetail)
		}
	}

	lookupResult := callTool(t, session, "lookup_session_events", map[string]any{
		"session_id": "session-1",
		"event_ids":  []any{testEventID},
	})
	assertStrictSuccess(t, lookupResult)
	lookup := asObject(t, asObject(t, lookupResult.StructuredContent)["readmodel"])
	if lookup["schema_version"] != readmodel.SchemaVersion ||
		lookup["requested_count"] != float64(1) ||
		lookup["found_count"] != float64(1) ||
		lookup["snapshot_scope"] != "current_ingestion" ||
		lookup["issue_snapshot_bound"] != false {
		t.Fatalf("event lookup metadata = %#v", lookup)
	}
	encodedLookup, err := json.Marshal(lookupResult.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encodedLookup), "belay.event.v1") {
		t.Fatalf("event evidence version missing: %s", encodedLookup)
	}
	for _, forbidden := range []string{
		"PRIVATE_INSTALLATION",
		"PRIVATE_RUN",
		"PRIVATE_RECORD",
		"PRIVATE_DEDUP",
		"PRIVATE_TOOL_CALL",
		"PRIVATE_DIFF_HASH",
		"installation_id",
		"run_id",
		"record_id",
		"deduplication_key",
		"tool_call_id",
		"diff_sha256",
	} {
		if strings.Contains(string(encodedLookup), forbidden) {
			t.Fatalf("event evidence exposed private field %q: %s", forbidden, encodedLookup)
		}
	}
}

func TestIssueEvidenceToolsRejectCursorConflictsAndMalformedInput(t *testing.T) {
	session := newTestClient(t, &testRepository{})
	tests := []struct {
		name string
		tool string
		args map[string]any
	}{
		{
			name: "list cursor plus limit",
			tool: "list_issues",
			args: map[string]any{"cursor": "opaque", "limit": 10},
		},
		{
			name: "get cursor plus view",
			tool: "get_issue",
			args: map[string]any{
				"issue_id":    testIssueID,
				"cursor":      "opaque",
				"view_cursor": "opaque",
			},
		},
		{
			name: "list limit too high",
			tool: "list_issues",
			args: map[string]any{"limit": 101},
		},
		{
			name: "malformed event id",
			tool: "lookup_session_events",
			args: map[string]any{
				"session_id": "session-1",
				"event_ids":  []any{"event-1"},
			},
		},
		{
			name: "unknown property",
			tool: "list_issues",
			args: map[string]any{"workflow_id": "forbidden"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertStrictToolError(t, callTool(t, session, test.tool, test.args), strictInvalidInput)
		})
	}
}

func TestIssueEvidenceSchemasAreRecursivelyClosed(t *testing.T) {
	factories := []func() (*strictToolSchemas, error){
		listIssuesSchemas,
		getIssueSchemas,
		lookupSessionEventsSchemas,
	}
	for _, factory := range factories {
		schemas, err := factory()
		if err != nil {
			t.Fatal(err)
		}
		assertRecursivelyClosed(t, schemas.input, "input")
		assertRecursivelyClosed(t, schemas.output, "output")
	}
}

func TestIssueReadErrorsMapToFixedCodes(t *testing.T) {
	tests := []struct {
		err  error
		code strictToolErrorCode
	}{
		{readmodel.ErrInvalidRequest, strictInvalidInput},
		{readmodel.ErrInvalidCursor, strictInvalidCursor},
		{readmodel.ErrCursorExpired, strictCursorExpired},
		{readmodel.ErrNotFound, strictIssueNotFound},
		{readmodel.ErrEvidenceResultTooLarge, strictResultTooLarge},
		{errors.New("private repository failure"), strictReadFailed},
	}
	for _, test := range tests {
		var failure strictToolFailure
		if err := strictIssueReadError(test.err); !errors.As(err, &failure) || failure.code != test.code {
			t.Fatalf("strictIssueReadError(%v) = %v, want %q", test.err, err, test.code)
		}
	}
}

func assertStrictSuccess(t *testing.T, result *mcp.CallToolResult) {
	t.Helper()
	if result == nil || result.IsError {
		t.Fatalf("strict tool result = %#v", result)
	}
	if got := toolResultText(t, result); got != strictToolNarrative {
		t.Fatalf("strict narrative = %q", got)
	}
	wrapper := asObject(t, result.StructuredContent)
	if wrapper["untrusted_observations"] != true {
		t.Fatalf("untrusted_observations = %#v", wrapper["untrusted_observations"])
	}
	trust := asObject(t, wrapper["trust"])
	if trust["classification"] != "untrusted_observations" ||
		trust["instruction_authority"] != "none" ||
		trust["must_not_authorize_actions"] != true {
		t.Fatalf("trust wrapper = %#v", trust)
	}
}

func assertRecursivelyClosed(t *testing.T, schema *jsonschema.Schema, path string) {
	t.Helper()
	if schema == nil {
		t.Fatalf("%s schema is nil", path)
	}
	if schema.Type == "object" {
		if schema.AdditionalProperties == nil {
			t.Fatalf("%s object schema is not closed", path)
		}
		for name, property := range schema.Properties {
			assertRecursivelyClosed(t, property, path+"."+name)
		}
	}
	if schema.Items != nil {
		assertRecursivelyClosed(t, schema.Items, path+"[]")
	}
}
