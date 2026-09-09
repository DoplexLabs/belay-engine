package localmcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/missionpack"
	"github.com/DoplexLabs/belay-engine/internal/presentation/readmodel"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type testMissionPackService struct {
	request missionpack.Request
	pack    missionpack.Pack
	err     error
}

func (s *testMissionPackService) Generate(
	_ context.Context,
	request missionpack.Request,
) (missionpack.Pack, error) {
	s.request = request
	return s.pack, s.err
}

type testMissionPackError string

func (err testMissionPackError) Error() string {
	return "private Mission Pack detail"
}

func (err testMissionPackError) MissionPackErrorKind() string {
	return string(err)
}

func TestMissionPackToolRegistersWithClosedSchemaAndTrustEnvelope(
	t *testing.T,
) {
	service := &testMissionPackService{pack: testMissionPack()}
	session := newMissionPackTestClient(t, service)

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var found *mcp.Tool
	for _, tool := range tools.Tools {
		if tool.Name == "get_mission_pack" {
			found = tool
			break
		}
	}
	if found == nil {
		t.Fatal("get_mission_pack was not registered")
	}
	if found.Annotations == nil || !found.Annotations.ReadOnlyHint {
		t.Fatalf("Mission Pack annotations = %#v", found.Annotations)
	}
	schemas, err := missionPackSchemas()
	if err != nil {
		t.Fatal(err)
	}
	if schemas.input.AdditionalProperties == nil ||
		schemas.output.AdditionalProperties == nil {
		t.Fatal("Mission Pack schemas are not closed")
	}

	result := callTool(t, session, "get_mission_pack", map[string]any{
		"cwd": "/tmp/example",
	})
	if result.IsError {
		t.Fatalf("get_mission_pack failed: %v", result.Content)
	}
	if service.request.Intent != missionpack.IntentGeneral {
		t.Fatalf("default intent = %q", service.request.Intent)
	}
	structured := asObject(t, result.StructuredContent)
	trust := asObject(t, structured["trust"])
	if trust["instruction_authority"] != "none" ||
		trust["must_not_authorize_actions"] != true {
		t.Fatalf("strict trust = %#v", trust)
	}
	pack := asObject(t, structured["readmodel"])
	packTrust := asObject(t, pack["trust"])
	if packTrust["instruction_authority"] != "none" ||
		packTrust["activation_required"] != true {
		t.Fatalf("pack trust = %#v", packTrust)
	}
	verification, ok := pack["verification"].([]any)
	if !ok || len(verification) != 1 {
		t.Fatalf("verification = %#v", pack["verification"])
	}
	command := asObject(t, verification[0])
	if _, exists := command["last_success"]; exists {
		t.Fatalf("configured-only command has last_success: %#v", command)
	}
	sources, ok := command["sources"].([]any)
	if !ok || len(sources) != 1 ||
		asObject(t, sources[0])["source_sha256"] != strings.Repeat("a", 64) {
		t.Fatalf("command sources = %#v", command["sources"])
	}
}

func TestMissionPackToolPassesHarnessAndExposesIt(t *testing.T) {
	pack := testMissionPack()
	pack.Harness = missionpack.HarnessClaude
	service := &testMissionPackService{pack: pack}
	session := newMissionPackTestClient(t, service)

	result := callTool(t, session, "get_mission_pack", map[string]any{
		"cwd":     "/tmp/example",
		"harness": "claude",
	})
	if result.IsError {
		t.Fatalf("get_mission_pack failed: %v", result.Content)
	}
	if service.request.Harness != missionpack.HarnessClaude {
		t.Fatalf("request harness = %q", service.request.Harness)
	}
	readModel := asObject(
		t,
		asObject(t, result.StructuredContent)["readmodel"],
	)
	if readModel["harness"] != "claude" {
		t.Fatalf("pack harness = %#v", readModel["harness"])
	}
}

func TestMissionPackToolAcceptsEmptyNonActivatablePack(t *testing.T) {
	pack := testMissionPack()
	pack.Status = "empty"
	pack.Trust.GuidanceState = "unavailable"
	pack.Trust.ActivationRequired = false
	pack.KnownTraps = []missionpack.GuidanceItem{}
	pack.OperatingRules = []missionpack.GuidanceItem{}
	pack.Verification = []missionpack.CommandItem{}
	pack.Completion = []missionpack.ChecklistItem{}
	pack.Context.Facts = []missionpack.ContextFact{{
		ID:      "fact_project_file",
		Kind:    missionpack.CanonicalFactFileWritten,
		Summary: "Frequently edited file: internal/example.go",
		Sources: []missionpack.SourceRef{},
	}}
	pack.RenderedMarkdown = "# Mission Pack\n\nNo actionable guidance.\n"
	session := newMissionPackTestClient(
		t,
		&testMissionPackService{pack: pack},
	)

	result := callTool(t, session, "get_mission_pack", map[string]any{
		"cwd": "/tmp/example",
	})
	if result.IsError {
		t.Fatalf("empty pack failed: %v", result.Content)
	}
	readModel := asObject(
		t,
		asObject(t, result.StructuredContent)["readmodel"],
	)
	trust := asObject(t, readModel["trust"])
	if readModel["status"] != "empty" ||
		trust["guidance_state"] != "unavailable" ||
		trust["activation_required"] != false {
		t.Fatalf("empty pack/trust = %#v / %#v", readModel, trust)
	}
}

func TestMissionPackToolRejectsInconsistentTrustStates(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*missionpack.Pack)
	}{
		{
			name: "empty proposal",
			mutate: func(pack *missionpack.Pack) {
				pack.Status = "empty"
				pack.Trust.GuidanceState = "proposal"
				pack.Trust.ActivationRequired = false
			},
		},
		{
			name: "empty activatable",
			mutate: func(pack *missionpack.Pack) {
				pack.Status = "empty"
				pack.Trust.GuidanceState = "unavailable"
				pack.Trust.ActivationRequired = true
			},
		},
		{
			name: "empty with actionable content",
			mutate: func(pack *missionpack.Pack) {
				pack.Status = "empty"
				pack.Trust.GuidanceState = "unavailable"
				pack.Trust.ActivationRequired = false
			},
		},
		{
			name: "partial unavailable",
			mutate: func(pack *missionpack.Pack) {
				pack.Status = "partial"
				pack.Trust.GuidanceState = "unavailable"
				pack.Trust.ActivationRequired = false
			},
		},
		{
			name: "wrong authority",
			mutate: func(pack *missionpack.Pack) {
				pack.Trust.InstructionAuthority = "agent"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pack := testMissionPack()
			test.mutate(&pack)
			session := newMissionPackTestClient(
				t,
				&testMissionPackService{pack: pack},
			)
			result := callTool(
				t,
				session,
				"get_mission_pack",
				map[string]any{"cwd": "/tmp/example"},
			)
			if !result.IsError ||
				missionPackResultErrorCode(result) !=
					string(strictReadFailed) {
				t.Fatalf(
					"result = %#v, want %s",
					result,
					strictReadFailed,
				)
			}
		})
	}
}

func TestMissionPackToolAcceptsEvidenceOnlyKnownTrap(t *testing.T) {
	pack := testMissionPack()
	pack.KnownTraps = []missionpack.GuidanceItem{{
		ID:               "trap_retry_loop",
		Kind:             "retry_loop",
		Title:            "Repeated command failure loop",
		SessionCount:     2,
		RequiresApproval: true,
		Sources: []missionpack.SourceRef{{
			Kind:       "issue",
			IssueID:    "iss_test",
			SessionKey: "ses_test",
		}},
	}}
	pack.OperatingRules = []missionpack.GuidanceItem{}
	session := newMissionPackTestClient(
		t,
		&testMissionPackService{pack: pack},
	)

	result := callTool(t, session, "get_mission_pack", map[string]any{
		"cwd": "/tmp/example",
	})
	if result.IsError {
		t.Fatalf("get_mission_pack failed: %v", result.Content)
	}
	readModel := asObject(
		t,
		asObject(t, result.StructuredContent)["readmodel"],
	)
	traps, ok := readModel["known_traps"].([]any)
	if !ok || len(traps) != 1 {
		t.Fatalf("known_traps = %#v", readModel["known_traps"])
	}
	if _, exists := asObject(t, traps[0])["guidance"]; exists {
		t.Fatalf("evidence-only trap serialized guidance: %#v", traps[0])
	}
	rules, ok := readModel["operating_rules"].([]any)
	if !ok || len(rules) != 0 {
		t.Fatalf("operating_rules = %#v", readModel["operating_rules"])
	}
}

func TestMissionPackToolRejectsOperatingRuleWithoutGuidance(t *testing.T) {
	pack := testMissionPack()
	pack.OperatingRules = []missionpack.GuidanceItem{{
		ID:               "rule_missing_guidance",
		Kind:             "semantic_rule",
		Title:            "Project operating rule",
		RequiresApproval: true,
		Sources:          []missionpack.SourceRef{},
	}}
	session := newMissionPackTestClient(
		t,
		&testMissionPackService{pack: pack},
	)

	result := callTool(t, session, "get_mission_pack", map[string]any{
		"cwd": "/tmp/example",
	})
	if !result.IsError ||
		missionPackResultErrorCode(result) != string(strictReadFailed) {
		t.Fatalf("result = %#v, want %s", result, strictReadFailed)
	}
}

func TestMissionPackToolRejectsInvalidInputs(t *testing.T) {
	session := newMissionPackTestClient(
		t,
		&testMissionPackService{pack: testMissionPack()},
	)
	tests := []struct {
		name string
		args map[string]any
	}{
		{name: "empty selector", args: map[string]any{}},
		{
			name: "relative cwd",
			args: map[string]any{"cwd": "relative/project"},
		},
		{
			name: "invalid intent",
			args: map[string]any{
				"cwd":    "/tmp/example",
				"intent": "deploy",
			},
		},
		{
			name: "invalid harness",
			args: map[string]any{
				"cwd":     "/tmp/example",
				"harness": "cursor",
			},
		},
		{
			name: "oversized cwd",
			args: map[string]any{
				"cwd": "/" + strings.Repeat("x", maxMissionPackCWDBytes),
			},
		},
		{
			name: "oversized issue",
			args: map[string]any{
				"issue_id": strings.Repeat(
					"x",
					maxMissionPackIssueIDBytes+1,
				),
			},
		},
		{
			name: "oversized task hint",
			args: map[string]any{
				"cwd": "/tmp/example",
				"task_hint": strings.Repeat(
					"x",
					maxMissionPackTaskHintRunes+1,
				),
			},
		},
		{
			name: "unknown field",
			args: map[string]any{
				"cwd":       "/tmp/example",
				"authorize": true,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := callTool(
				t,
				session,
				"get_mission_pack",
				test.args,
			)
			if !result.IsError ||
				missionPackResultErrorCode(result) !=
					string(strictInvalidInput) {
				t.Fatalf(
					"result = %#v, want %s",
					result,
					strictInvalidInput,
				)
			}
		})
	}
}

func TestMissionPackToolMapsFailuresToFixedCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want strictToolErrorCode
	}{
		{
			name: "validation",
			err:  testMissionPackError("invalid_request"),
			want: strictInvalidInput,
		},
		{
			name: "project not found",
			err:  missionpack.ErrProjectNotFound,
			want: strictIssueNotFound,
		},
		{
			name: "issue not found",
			err:  testMissionPackError("issue_not_found"),
			want: strictIssueNotFound,
		},
		{
			name: "project mismatch",
			err:  missionpack.ErrProjectMismatch,
			want: strictInvalidInput,
		},
		{
			name: "deadline",
			err:  context.DeadlineExceeded,
			want: strictReadTimeout,
		},
		{
			name: "local failure",
			err:  errors.New("private path /Users/example/secret"),
			want: strictReadFailed,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := newMissionPackTestClient(
				t,
				&testMissionPackService{err: test.err},
			)
			result := callTool(
				t,
				session,
				"get_mission_pack",
				map[string]any{"issue_id": "csi_test"},
			)
			if got := missionPackResultErrorCode(result); got !=
				string(test.want) {
				t.Fatalf("error code = %q, want %q", got, test.want)
			}
			if strings.Contains(
				missionPackResultErrorCode(result),
				"private",
			) {
				t.Fatal("private service detail leaked")
			}
		})
	}
}

func newMissionPackTestClient(
	t *testing.T,
	service MissionPackService,
) *mcp.ClientSession {
	t.Helper()
	repository := &testRepository{}
	server, err := New(
		readmodel.New(
			repository,
			readmodel.WithIssueRepository(repository),
			readmodel.WithIssueCursorCodec(testIssueCursorCodec{}),
			readmodel.WithCostIssueRepository(repository),
			readmodel.WithClock(testTime),
		),
		WithMissionPackService(service),
	)
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.mcp.Connect(
		context.Background(),
		serverTransport,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(
		&mcp.Implementation{Name: "mission-pack-test", Version: "1"},
		nil,
	)
	clientSession, err := client.Connect(
		context.Background(),
		clientTransport,
		nil,
	)
	if err != nil {
		_ = serverSession.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	})
	return clientSession
}

func testMissionPack() missionpack.Pack {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	return missionpack.Pack{
		SchemaVersion:    missionpack.SchemaVersion,
		GeneratorVersion: missionpack.GeneratorVersion,
		PackID: "mpk_" +
			strings.Repeat("a", 52),
		GeneratedAt: now,
		Project: missionpack.Project{
			Label:        "example",
			IdentityKind: "remote",
			Branch:       "main",
			Worktree:     "example",
		},
		Intent: missionpack.IntentGeneral,
		Status: "ready",
		Trust: missionpack.Trust{
			InstructionAuthority: "none",
			GuidanceState:        "proposal",
			EvidenceState:        "untrusted",
			ActivationRequired:   true,
		},
		SourceState: missionpack.SourceState{
			TranscriptGeneration: 2,
			AnalyzedGeneration:   2,
			AnalysisStatus:       missionpack.AnalysisStatusCurrent,
			DataThrough:          now,
		},
		Context: missionpack.Context{
			Harnesses: []string{"codex"},
			Facts:     []missionpack.ContextFact{},
		},
		KnownTraps:     []missionpack.GuidanceItem{},
		OperatingRules: []missionpack.GuidanceItem{},
		Verification: []missionpack.CommandItem{{
			ID:               "cmd_test",
			Command:          "go test ./...",
			Class:            "test",
			Configured:       true,
			RequiresApproval: true,
			Sources: []missionpack.SourceRef{{
				Kind:         "project_config",
				ProjectFile:  "Makefile",
				SourceSHA256: strings.Repeat("a", 64),
			}},
		}},
		Completion:       []missionpack.ChecklistItem{},
		Warnings:         []missionpack.Warning{},
		EstimatedTokens:  4,
		RenderedMarkdown: "# Mission Pack\n",
	}
}

func missionPackResultErrorCode(result *mcp.CallToolResult) string {
	if result == nil || !result.IsError || len(result.Content) != 1 {
		return ""
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		return ""
	}
	return text.Text
}
