// Package issueintel defines Belay Local's cost-ranked transcript issue model.
package issueintel

import (
	"time"

	"github.com/DoplexLabs/belay-engine/internal/transcript"
)

const (
	DetectorRetryLoop                  = "retry_loop"
	DetectorRecurringError             = "recurring_error"
	DetectorRepeatedCorrection         = "repeated_correction"
	DetectorDoneWithoutVerification    = "done_without_verification"
	DetectorPermissionChurn            = "permission_churn"
	DetectorColdStartCost              = "cold_start_cost"
	DetectorFileThrash                 = "file_thrash"
	DetectorCompactionBeforeCompletion = "compaction_before_completion"
)

type Cost struct {
	WastedMinutes float64  `json:"wasted_minutes"`
	WastedTokens  int64    `json:"wasted_tokens"`
	WastedUSD     *float64 `json:"wasted_usd"`
	LowerBound    bool     `json:"lower_bound"`
}

type Project struct {
	Identity string `json:"identity"`
	Path     string `json:"path"`
}

type SessionRef struct {
	SessionKey string    `json:"session_key"`
	Agent      string    `json:"agent"`
	StartedAt  time.Time `json:"started_at"`
	EndedAt    time.Time `json:"ended_at"`
}

type Citation struct {
	SessionKey      string    `json:"session_key"`
	TurnIndex       int64     `json:"turn_index"`
	OccurredAt      time.Time `json:"occurred_at"`
	SourceFileID    string    `json:"source_file_id"`
	JSONLByteOffset int64     `json:"jsonl_byte_offset"`
}

type Excerpt struct {
	Citation Citation        `json:"citation"`
	Role     transcript.Role `json:"role"`
	ToolName string          `json:"tool_name,omitempty"`
	Text     string          `json:"text"`
}

type TrendWeek struct {
	WeekStart time.Time `json:"week_start"`
	Count     int       `json:"count"`
}

type SuggestedFix struct {
	Kind       string `json:"kind"`
	TargetFile string `json:"target_file"`
	Rationale  string `json:"rationale"`
}

type Issue struct {
	IssueID      string       `json:"issue_id"`
	DetectorID   string       `json:"detector_id"`
	Fingerprint  string       `json:"fingerprint"`
	Headline     string       `json:"headline"`
	Cost         Cost         `json:"cost"`
	SessionCount int          `json:"session_count"`
	Sessions     []SessionRef `json:"sessions"`
	FirstSeen    time.Time    `json:"first_seen"`
	LastSeen     time.Time    `json:"last_seen"`
	Trend        []TrendWeek  `json:"trend"`
	Excerpts     []Excerpt    `json:"excerpts"`
	Project      Project      `json:"project"`
	SuggestedFix SuggestedFix `json:"suggested_fix"`
}

type ProjectConfig struct {
	VerificationCommands  []string `json:"verification_commands,omitempty"`
	HasClaudeInstructions bool     `json:"has_claude_instructions"`
	HasCodexInstructions  bool     `json:"has_codex_instructions"`
}

type Session struct {
	Metadata transcript.Session `json:"metadata"`
	Turns    []transcript.Turn  `json:"turns"`
}

type ProjectInput struct {
	Project  Project       `json:"project"`
	Config   ProjectConfig `json:"config"`
	Sessions []Session     `json:"sessions"`
	Now      time.Time     `json:"now"`
}

type CorrectionCandidate struct {
	CandidateID string    `json:"candidate_id"`
	Project     Project   `json:"project"`
	Citation    Citation  `json:"citation"`
	Text        string    `json:"text"`
	Marker      string    `json:"marker,omitempty"`
	ShortTurn   bool      `json:"short_turn"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type Analysis struct {
	Issues               []Issue               `json:"issues"`
	CorrectionCandidates []CorrectionCandidate `json:"correction_candidates"`
}

type Query struct {
	Limit           int
	ProjectIdentity string
	DetectorID      string
}
