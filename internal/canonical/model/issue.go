package model

import "time"

type ScopeQuality string

const (
	ScopeResolved ScopeQuality = "resolved"
	ScopeLexical  ScopeQuality = "lexical"
	ScopeUnscoped ScopeQuality = "unscoped"
	ScopeConflict ScopeQuality = "conflict"
)

type AnalysisStatus string

const (
	AnalysisCurrent   AnalysisStatus = "current"
	AnalysisPending   AnalysisStatus = "pending"
	AnalysisFailed    AnalysisStatus = "failed"
	AnalysisTruncated AnalysisStatus = "truncated"
)

type EventEnrichment struct {
	CommandSignatureID string `json:"command_signature_id,omitempty"`
	CommandClass       string `json:"command_class,omitempty"`
	PermissionClass    string `json:"permission_class,omitempty"`
	Version            string `json:"version"`
}

type DetectorProvenance struct {
	DetectorID         string `json:"detector_id"`
	DetectorVersion    string `json:"detector_version"`
	FingerprintVersion string `json:"fingerprint_version"`
	ProjectionVersion  string `json:"projection_version"`
}

type IssueEvidence struct {
	CitedEventIDs []string `json:"cited_event_ids"`
	Dimensions    []string `json:"dimensions,omitempty"`
}

type IssueOccurrence struct {
	OccurrenceID        string             `json:"occurrence_id"`
	IssueID             string             `json:"issue_id"`
	FingerprintID       string             `json:"fingerprint_id"`
	FingerprintVersion  string             `json:"fingerprint_version"`
	Origin              string             `json:"origin"`
	OriginRecordID      string             `json:"origin_record_id,omitempty"`
	SessionID           string             `json:"session_id"`
	Harness             string             `json:"harness"`
	Provenance          DetectorProvenance `json:"provenance"`
	Category            string             `json:"category"`
	TitleCode           string             `json:"title_code"`
	Severity            string             `json:"severity"`
	Confidence          string             `json:"confidence"`
	ScopeQuality        ScopeQuality       `json:"scope_quality"`
	FirstObservedAt     time.Time          `json:"first_observed_at"`
	LastObservedAt      time.Time          `json:"last_observed_at"`
	EvidenceComplete    bool               `json:"evidence_complete"`
	RetainedHistoryOnly bool               `json:"retained_history_only"`
	AnalysisStatus      AnalysisStatus     `json:"analysis_status"`
	AnalysisGeneration  int64              `json:"analysis_generation"`
	Evidence            IssueEvidence      `json:"evidence"`
}

type IssueSummary struct {
	IssueID             string         `json:"issue_id"`
	FingerprintID       string         `json:"fingerprint_id"`
	FingerprintVersion  string         `json:"fingerprint_version"`
	Origin              string         `json:"origin"`
	DetectorID          string         `json:"detector_id"`
	DetectorVersion     string         `json:"detector_version"`
	Category            string         `json:"category"`
	TitleCode           string         `json:"title_code"`
	Severity            string         `json:"severity"`
	Confidence          string         `json:"confidence"`
	ScopeQuality        ScopeQuality   `json:"scope_quality"`
	FirstObservedAt     time.Time      `json:"first_observed_at"`
	LastObservedAt      time.Time      `json:"last_observed_at"`
	OccurrenceCount     int            `json:"occurrence_count"`
	SessionCount        int            `json:"session_count"`
	Harnesses           []string       `json:"harnesses"`
	AnalysisStatus      AnalysisStatus `json:"analysis_status"`
	EvidenceComplete    bool           `json:"evidence_complete"`
	RetainedHistoryOnly bool           `json:"retained_history_only"`
}

type IssueAnalysisCoverage struct {
	CurrentSessions   int       `json:"current_sessions"`
	PendingSessions   int       `json:"pending_sessions"`
	FailedSessions    int       `json:"failed_sessions"`
	TruncatedSessions int       `json:"truncated_sessions"`
	UnscopedSessions  int       `json:"unscoped_sessions"`
	AnalysisThrough   time.Time `json:"analysis_through"`
	Complete          bool      `json:"complete"`
}

type IssueFilter struct {
	Severity       string
	Category       string
	Harness        string
	Origin         string
	AnalysisStatus AnalysisStatus
	ObservedAfter  *time.Time
	Recurrence     string
	SessionID      string
	FingerprintID  string
	IssueID        string
}

type IssuePosition struct {
	SeverityRank int
	Repeated     bool
	LastObserved time.Time
	IssueID      string
}

type IssueQuery struct {
	Filter   IssueFilter
	Limit    int
	Snapshot int64
	IssuedAt time.Time
	Cursor   *IssuePosition
}

type IssuePage struct {
	Data     []IssueSummary        `json:"data"`
	Analysis IssueAnalysisCoverage `json:"analysis"`
	Snapshot int64                 `json:"snapshot"`
	HasMore  bool                  `json:"has_more"`
}

type IssueOccurrencePosition struct {
	LastObserved time.Time
	OccurrenceID string
}

type IssueOccurrenceQuery struct {
	IssueID  string
	Limit    int
	Snapshot int64
	IssuedAt time.Time
	Cursor   *IssueOccurrencePosition
}

type IssueOccurrencePage struct {
	Data     []IssueOccurrence `json:"data"`
	Snapshot int64             `json:"snapshot"`
	HasMore  bool              `json:"has_more"`
}
