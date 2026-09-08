package readmodel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/canonical/model"
)

const (
	IssueCatalogVersion      = "belay.issue-explanations.v1"
	maxEvidenceResultBytes   = 2 << 20
	evidenceSnapshotScope    = "current_ingestion"
	evidenceMissingSemantics = "unavailable_from_selected_retained_session"
)

var ErrEvidenceResultTooLarge = errors.New("event evidence result exceeds the bounded read limit")

type IssueSelection struct {
	AttentionKind        string `json:"attention_kind"`
	Experimental         string `json:"experimental"`
	IncludesEvidenceGaps bool   `json:"includes_evidence_gaps"`
	IncludesExperimental bool   `json:"includes_experimental"`
}

type IssueCatalogMetadata struct {
	CatalogVersion       string `json:"catalog_version"`
	CatalogStatus        string `json:"catalog_status"`
	TitleCode            string `json:"title_code"`
	ObservationStatement string `json:"observation_statement"`
	Caveat               string `json:"caveat"`
	NextEvidenceAction   string `json:"next_evidence_action"`
}

type EventEvidenceLookupRequest struct {
	SessionID string
	EventIDs  []string
}

type EventEvidenceLookup struct {
	SchemaVersion       string             `json:"schema_version"`
	Data                []MCPEventEvidence `json:"data"`
	RequestedCount      int                `json:"requested_count"`
	FoundCount          int                `json:"found_count"`
	MissingCount        int                `json:"missing_count"`
	MissingEventIDs     []string           `json:"missing_event_ids"`
	DataThrough         time.Time          `json:"data_through"`
	SnapshotScope       string             `json:"snapshot_scope"`
	IssueSnapshotBound  bool               `json:"issue_snapshot_bound"`
	EvidenceEvaluatedAt time.Time          `json:"evidence_evaluated_at"`
	MissingSemantics    string             `json:"missing_semantics"`
}

type MCPEventEvidence struct {
	SchemaVersion string              `json:"schema_version"`
	EventID       string              `json:"event_id"`
	OccurredAt    time.Time           `json:"occurred_at"`
	ObservedAt    time.Time           `json:"observed_at"`
	SessionID     string              `json:"session_id"`
	Source        MCPEventSource      `json:"source"`
	Observation   MCPEventObservation `json:"observation"`
	Coverage      MCPEventCoverage    `json:"coverage"`
	Redaction     MCPEventRedaction   `json:"redaction"`
	Historical    MCPEventHistorical  `json:"historical"`
}

type MCPEventSource struct {
	Engine         string `json:"engine"`
	EngineVersion  string `json:"engine_version"`
	SchemaVersion  string `json:"schema_version"`
	RecordType     string `json:"record_type"`
	Kind           string `json:"kind"`
	Agent          string `json:"agent"`
	AdapterVersion string `json:"adapter_version"`
	Sequence       int64  `json:"sequence"`
}

type MCPEventObservation struct {
	Type       string            `json:"type"`
	Actor      string            `json:"actor"`
	Action     string            `json:"action"`
	Outcome    string            `json:"outcome"`
	ExitCode   *int              `json:"exit_code,omitempty"`
	DurationMS *int64            `json:"duration_ms,omitempty"`
	Summary    string            `json:"summary,omitempty"`
	Resource   *MCPEventResource `json:"resource,omitempty"`
	Details    *MCPEventDetails  `json:"details,omitempty"`
}

type MCPEventResource struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type MCPEventDetails struct {
	Decision         string   `json:"decision,omitempty"`
	ApprovalRequired *bool    `json:"approval_required,omitempty"`
	ApprovalDecision string   `json:"approval_decision,omitempty"`
	MCPServer        string   `json:"mcp_server,omitempty"`
	MCPTool          string   `json:"mcp_tool,omitempty"`
	Model            string   `json:"model,omitempty"`
	ModelProvider    string   `json:"model_provider,omitempty"`
	CLIVersion       string   `json:"cli_version,omitempty"`
	SubAgent         string   `json:"sub_agent,omitempty"`
	DiffBytes        int      `json:"diff_bytes,omitempty"`
	Tags             []string `json:"tags,omitempty"`
}

type MCPEventCoverage struct {
	Depth      string `json:"depth"`
	Confidence string `json:"confidence"`
}

type MCPEventRedaction struct {
	PolicyVersion  string `json:"policy_version"`
	FieldsRemoved  int    `json:"fields_removed"`
	SecretsRemoved int    `json:"secrets_removed"`
}

type MCPEventHistorical struct {
	IsHistorical         bool   `json:"is_historical"`
	ReconstructionSource string `json:"reconstruction_source,omitempty"`
}

func normalizedIssueSelection(request IssueListRequest) IssueSelection {
	return IssueSelection{
		AttentionKind:        request.AttentionKind,
		Experimental:         request.Experimental,
		IncludesEvidenceGaps: request.AttentionKind != model.AttentionKindIssue,
		IncludesExperimental: request.Experimental != model.ExperimentalStable,
	}
}

func issueCatalog(titleCode string) IssueCatalogMetadata {
	result := IssueCatalogMetadata{
		CatalogVersion: IssueCatalogVersion,
		CatalogStatus:  "known",
		TitleCode:      titleCode,
	}
	switch titleCode {
	case "issue.explicit_command_failure":
		result.ObservationStatement = "The source explicitly reported a failed command result."
		result.Caveat = "A reported command failure does not by itself establish root cause or whether a later attempt succeeded."
		result.NextEvidenceAction = "inspect_cited_events"
	case "issue.repeated_command_attempts":
		result.ObservationStatement = "The same private command signature was observed multiple times in one bounded interval."
		result.Caveat = "Repeated attempts do not by themselves establish a stall, incorrect behavior, or shared root cause."
		result.NextEvidenceAction = "inspect_matching_sessions"
	case "issue.explicit_permission_denial":
		result.ObservationStatement = "The source explicitly reported a denied permission event."
		result.Caveat = "A denied permission may reflect an intentional policy boundary and does not by itself establish a defect."
		result.NextEvidenceAction = "inspect_cited_events"
	case "issue.verification_not_observed":
		result.ObservationStatement = "A supported live session ended without the required verification evidence."
		result.Caveat = "Evidence not observed under supported retained coverage is not proof that verification did not occur elsewhere."
		result.NextEvidenceAction = "inspect_verification_events"
	case "issue.unresolved_verification_failure_at_completion":
		result.ObservationStatement = "A verification command explicitly failed and no later successful verification was observed before session end."
		result.Caveat = "This statement is bounded to the retained evidence for that session and does not establish the current system state."
		result.NextEvidenceAction = "inspect_verification_events"
	case "issue.numbat_finding":
		result.ObservationStatement = "A retained upstream Numbat finding was reported."
		result.Caveat = "Belay preserves this upstream positive finding without inferring additional absence, cause, or remediation claims."
		result.NextEvidenceAction = "inspect_cited_events"
	default:
		result.CatalogStatus = "unknown"
		result.ObservationStatement = "A configured deterministic detector reported retained evidence."
		result.Caveat = "No fixed Belay explanation is available for this title code."
		result.NextEvidenceAction = "inspect_cited_events"
	}
	return result
}

func (s *Service) LookupSessionEventEvidence(
	ctx context.Context,
	request EventEvidenceLookupRequest,
) (EventEvidenceLookup, error) {
	sessionID, eventIDs, err := normalizeEventLookupRequest(
		request.SessionID,
		request.EventIDs,
	)
	if err != nil {
		return EventEvidenceLookup{}, err
	}
	if s == nil || s.issueRepository == nil {
		return EventEvidenceLookup{}, capabilityUnavailable()
	}

	data := make([]MCPEventEvidence, 0, len(eventIDs))
	encodedBytes := 0
	summary, err := s.issueRepository.VisitSessionEvents(
		ctx,
		model.EventLookupQuery{SessionID: sessionID, EventIDs: eventIDs},
		func(event model.Event) error {
			evidence := mcpEventEvidence(sessionID, event)
			encoded, err := json.Marshal(evidence)
			if err != nil {
				return errors.New("encode event evidence")
			}
			if encodedBytes+len(encoded)+1 > maxEvidenceResultBytes {
				return ErrEvidenceResultTooLarge
			}
			encodedBytes += len(encoded) + 1
			data = append(data, evidence)
			return nil
		},
	)
	if err != nil {
		return EventEvidenceLookup{}, err
	}
	result := EventEvidenceLookup{
		SchemaVersion:       SchemaVersion,
		Data:                nonNil(data),
		RequestedCount:      summary.RequestedCount,
		FoundCount:          summary.FoundCount,
		MissingCount:        summary.MissingCount,
		MissingEventIDs:     nonNil(summary.MissingEventIDs),
		DataThrough:         summary.DataThrough,
		SnapshotScope:       evidenceSnapshotScope,
		IssueSnapshotBound:  false,
		EvidenceEvaluatedAt: s.nowUTC(),
		MissingSemantics:    evidenceMissingSemantics,
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return EventEvidenceLookup{}, errors.New("encode event evidence result")
	}
	if len(encoded) > maxEvidenceResultBytes {
		return EventEvidenceLookup{}, ErrEvidenceResultTooLarge
	}
	return result, nil
}

func normalizeEventLookupRequest(
	sessionID string,
	values []string,
) (string, []string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || len(sessionID) > 256 {
		return "", nil, invalidRequest("session_id is required and must not exceed 256 bytes")
	}
	if len(values) == 0 || len(values) > model.MaxEventLookupIDs {
		return "", nil, invalidRequest("event_id must be supplied between one and 50 times")
	}
	eventIDs := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		eventID := strings.TrimSpace(value)
		if !model.IsCanonicalUUIDv7(eventID) {
			return "", nil, invalidRequest("event_id is malformed")
		}
		if _, exists := seen[eventID]; exists {
			continue
		}
		seen[eventID] = struct{}{}
		eventIDs = append(eventIDs, eventID)
	}
	return sessionID, eventIDs, nil
}

func mcpEventEvidence(sessionID string, event model.Event) MCPEventEvidence {
	result := MCPEventEvidence{
		SchemaVersion: event.SchemaVersion,
		EventID:       event.EventID,
		OccurredAt:    event.OccurredAt,
		ObservedAt:    event.ObservedAt,
		SessionID:     sessionID,
		Source: MCPEventSource{
			Engine:         event.Source.Engine,
			EngineVersion:  event.Source.EngineVersion,
			SchemaVersion:  event.Source.SchemaVersion,
			RecordType:     event.Source.RecordType,
			Kind:           event.Source.Kind,
			Agent:          event.Source.Agent,
			AdapterVersion: event.Source.AdapterVersion,
			Sequence:       event.Source.Sequence,
		},
		Observation: MCPEventObservation{
			Type:       event.Observation.Type,
			Actor:      event.Observation.Actor,
			Action:     event.Observation.Action,
			Outcome:    event.Observation.Outcome,
			ExitCode:   event.Observation.ExitCode,
			DurationMS: event.Observation.DurationMS,
			Summary:    event.Observation.Summary,
		},
		Coverage: MCPEventCoverage{
			Depth:      event.Coverage.Depth,
			Confidence: event.Coverage.Confidence,
		},
		Redaction: MCPEventRedaction{
			PolicyVersion:  event.Redaction.PolicyVersion,
			FieldsRemoved:  event.Redaction.FieldsRemoved,
			SecretsRemoved: event.Redaction.SecretsRemoved,
		},
		Historical: MCPEventHistorical{
			IsHistorical:         event.Historical.IsHistorical,
			ReconstructionSource: event.Historical.ReconstructionSource,
		},
	}
	if event.Observation.Resource != nil {
		result.Observation.Resource = &MCPEventResource{
			Kind: event.Observation.Resource.Kind,
			Name: event.Observation.Resource.Name,
		}
	}
	if event.Observation.Details != nil {
		result.Observation.Details = &MCPEventDetails{
			Decision:         event.Observation.Details.Decision,
			ApprovalRequired: event.Observation.Details.ApprovalRequired,
			ApprovalDecision: event.Observation.Details.ApprovalDecision,
			MCPServer:        event.Observation.Details.MCPServer,
			MCPTool:          event.Observation.Details.MCPTool,
			Model:            event.Observation.Details.Model,
			ModelProvider:    event.Observation.Details.ModelProvider,
			CLIVersion:       event.Observation.Details.CLIVersion,
			SubAgent:         event.Observation.Details.SubAgent,
			DiffBytes:        event.Observation.Details.DiffBytes,
			Tags:             append([]string(nil), event.Observation.Details.Tags...),
		}
	}
	return result
}
