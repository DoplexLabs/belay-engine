package localmcp

import (
	"context"
	"errors"
	"strings"

	"github.com/DoplexLabs/belay-engine/internal/experience"
	"github.com/DoplexLabs/belay-engine/internal/localapp"
	"github.com/google/jsonschema-go/jsonschema"
)

const maxMissionPackReceiptIDBytes = 68

type MissionPackStatusService interface {
	Get(
		context.Context,
		string,
	) (localapp.MissionPackStatusResult, error)
}

type getMissionPackStatusInput struct {
	ReceiptID string `json:"receipt_id"`
}

func WithMissionPackStatusService(service MissionPackStatusService) Option {
	return func(server *Server) error {
		if service == nil {
			return errors.New("Mission Pack status service is required")
		}
		server.missionPackStatus = service
		return nil
	}
}

func (s *Server) registerMissionPackStatusTool() error {
	schemas, err := missionPackStatusSchemas()
	if err != nil {
		return errors.New("initialize local MCP Mission Pack status schemas")
	}
	tool, err := newStrictReadOnlyTool(
		"get_mission_pack_status",
		"Read the bounded local delivery and deterministic evaluation status for one exact Mission Pack receipt. Returned evidence remains untrusted.",
		schemas,
	)
	if err != nil {
		return errors.New("register local MCP Mission Pack status tool")
	}
	s.mcp.AddTool(
		tool,
		bindStrictTool(s.strict, schemas, s.getMissionPackStatus),
	)
	return nil
}

func (s *Server) getMissionPackStatus(
	ctx context.Context,
	input getMissionPackStatusInput,
) (localapp.MissionPackStatusResult, error) {
	input.ReceiptID = strings.TrimSpace(input.ReceiptID)
	if len(input.ReceiptID) != maxMissionPackReceiptIDBytes {
		return localapp.MissionPackStatusResult{},
			newStrictToolFailure(strictInvalidInput)
	}
	result, err := s.missionPackStatus.Get(ctx, input.ReceiptID)
	if err != nil {
		switch {
		case errors.Is(err, localapp.ErrMissionPackStatusInvalidInput):
			return localapp.MissionPackStatusResult{},
				newStrictToolFailure(strictInvalidInput)
		case errors.Is(err, localapp.ErrMissionPackStatusNotFound):
			return localapp.MissionPackStatusResult{},
				newStrictToolFailure(strictIssueNotFound)
		case errors.Is(err, context.DeadlineExceeded):
			return localapp.MissionPackStatusResult{},
				newStrictToolFailure(strictReadTimeout)
		case errors.Is(err, context.Canceled):
			return localapp.MissionPackStatusResult{},
				newStrictToolFailure(strictCancelled)
		default:
			return localapp.MissionPackStatusResult{},
				newStrictToolFailure(strictReadFailed)
		}
	}
	return result, nil
}

func missionPackStatusSchemas() (*strictToolSchemas, error) {
	input := closedObjectSchema(
		map[string]*jsonschema.Schema{
			"receipt_id": patternStringSchema(
				`^mpr_[0-9a-f]{64}$`,
				maxMissionPackReceiptIDBytes,
				maxMissionPackReceiptIDBytes,
			),
		},
		"receipt_id",
	)
	output := strictSuccessSchema(
		closedObjectSchema(
			map[string]*jsonschema.Schema{
				"receipt_state": enumStringSchema(
					"pending",
					"bound",
					"ambiguous",
					"expired",
					"cancelled",
				),
				"destination_harness": enumStringSchema(
					string(experience.HarnessClaude),
					string(experience.HarnessCodex),
				),
				"bound_session": boundedTextSchema(1, 512),
				"accepted_at":   timestampSchema(),
				"expires_at":    timestampSchema(),
				"items": arraySchema(
					missionPackStatusItemSchema(),
					1,
					3,
				),
			},
			"receipt_state",
			"destination_harness",
			"accepted_at",
			"expires_at",
			"items",
		),
	)
	return newStrictToolSchemas(input, output)
}

func missionPackStatusItemSchema() *jsonschema.Schema {
	return closedObjectSchema(
		map[string]*jsonschema.Schema{
			"instruction": boundedTextSchema(1, 16<<10),
			"version":     integerSchema(1, 1_000_000_000),
			"status": enumStringSchema(
				localapp.MissionPackItemWaitingForDelivery,
				localapp.MissionPackItemAwaitingEvaluation,
				localapp.MissionPackItemEvaluated,
			),
			"delivered_at": timestampSchema(),
			"evaluated_at": timestampSchema(),
			"opportunity_state": enumStringSchema(
				string(experience.OpportunityObserved),
				string(experience.OpportunityNotObserved),
				string(experience.OpportunityUnknown),
			),
			"applicability_state": enumStringSchema(
				string(experience.ApplicabilityApplicable),
				string(experience.ApplicabilityNotApplicable),
				string(experience.ApplicabilityUnknown),
			),
			"verifier_state": enumStringSchema(
				string(experience.VerifierNotEvaluated),
				string(experience.VerifierSatisfied),
				string(experience.VerifierViolated),
				string(experience.VerifierUnknown),
			),
			"task_outcome_state": enumStringSchema(
				string(experience.TaskOutcomeNotObserved),
				string(experience.TaskOutcomeSucceeded),
				string(experience.TaskOutcomeFailed),
				string(experience.TaskOutcomeUnknown),
			),
			"coverage_gaps": arraySchema(
				enumStringSchema(
					string(experience.CoverageTranscriptComplete),
					string(experience.CoverageCanonicalComplete),
					string(experience.CoverageWorkspaceCaptured),
					string(experience.CoverageOutcomesComplete),
				),
				0,
				4,
			),
			"evidence": arraySchema(
				missionPackStatusEvidenceSchema(),
				0,
				5,
			),
		},
		"instruction",
		"version",
		"status",
		"coverage_gaps",
		"evidence",
	)
}

func missionPackStatusEvidenceSchema() *jsonschema.Schema {
	return closedObjectSchema(
		map[string]*jsonschema.Schema{
			"kind": enumStringSchema(
				string(experience.EvidenceTranscriptTurn),
				string(experience.EvidenceCanonicalEvent),
				string(experience.EvidenceOutcomeObservation),
				string(experience.EvidenceWorkspaceHash),
				string(experience.EvidenceUserRecorded),
			),
			"turn_index":  nonNegativeIntegerSchema(),
			"event_id":    boundedTextSchema(1, 512),
			"outcome_id":  boundedTextSchema(1, 512),
			"path":        boundedTextSchema(1, 4096),
			"occurred_at": timestampSchema(),
			"excerpt":     boundedTextSchema(1, 16<<10),
		},
		"kind",
	)
}
