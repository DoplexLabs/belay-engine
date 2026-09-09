package transcriptissues

import (
	"time"

	"github.com/DoplexLabs/belay-engine/internal/issueintel"
	"github.com/DoplexLabs/belay-engine/internal/transcript"
)

const (
	Version         = "belay.transcript-issues.v1"
	maxProjectTurns = 500_000
	maxExcerpts     = 5
)

type preparedProject struct {
	project  issueintel.Project
	config   issueintel.ProjectConfig
	sessions []preparedSession
	now      time.Time
}

type preparedSession struct {
	metadata transcript.Session
	turns    []transcript.Turn
}

type observation struct {
	detectorID  string
	fingerprint string
	subject     string
	cost        issueintel.Cost
	session     issueintel.SessionRef
	firstSeen   time.Time
	lastSeen    time.Time
	occurredAt  time.Time
	excerpts    []issueintel.Excerpt
	fix         issueintel.SuggestedFix
}

type observationGroup struct {
	detectorID  string
	fingerprint string
	subject     string
	cost        issueintel.Cost
	sessions    map[string]issueintel.SessionRef
	firstSeen   time.Time
	lastSeen    time.Time
	occurrences []time.Time
	excerpts    []issueintel.Excerpt
	fix         issueintel.SuggestedFix
	usdKnown    bool
}
