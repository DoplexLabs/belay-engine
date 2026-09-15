package readmodel

import (
	"context"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/DoplexLabs/belay-engine/internal/transcript"
)

const TranscriptStatusSchemaVersion = "belay.transcript-status.v1"

const (
	defaultTranscriptSessionLimit = 10
	activeTranscriptWindow        = 5 * time.Minute
	recentTranscriptWindow        = 24 * time.Hour
	maxProjectLabelRunes          = 80
)

type TranscriptRepository interface {
	TranscriptCoverage(context.Context) (transcript.CoverageCounts, error)
	QueryTranscriptSessions(
		context.Context,
		transcript.SessionQuery,
	) ([]transcript.Session, error)
}

type TranscriptSessionStatus struct {
	SessionKey     string                     `json:"session_key"`
	Agent          string                     `json:"agent"`
	Project        string                     `json:"project"`
	StartedAt      time.Time                  `json:"started_at"`
	EndedAt        *time.Time                 `json:"ended_at"`
	LastActivityAt time.Time                  `json:"last_activity_at"`
	Coverage       transcript.SessionCoverage `json:"coverage"`
	TurnCount      int                        `json:"turn_count"`
	Active         bool                       `json:"active"`
}

type TranscriptCoverageStatus struct {
	WithTranscript int `json:"with_transcript"`
	Partial        int `json:"partial"`
	Without        int `json:"without_transcript"`
	TranscriptOnly int `json:"transcript_only"`
}

type TranscriptStatus struct {
	SchemaVersion string                    `json:"schema_version"`
	Coverage      TranscriptCoverageStatus  `json:"coverage"`
	Sessions      []TranscriptSessionStatus `json:"sessions"`
}

func WithTranscriptRepository(repository TranscriptRepository) Option {
	return func(service *Service) {
		if service != nil {
			service.transcriptRepository = repository
			if insights, ok := repository.(UserInsightRepository); ok &&
				service.userInsightRepository == nil {
				service.userInsightRepository = insights
			}
		}
	}
}

func (s *Service) GetTranscriptStatus(
	ctx context.Context,
) (TranscriptStatus, error) {
	if s == nil || s.transcriptRepository == nil {
		return TranscriptStatus{}, capabilityUnavailable()
	}
	coverage, err := s.transcriptRepository.TranscriptCoverage(ctx)
	if err != nil {
		return TranscriptStatus{}, err
	}
	sessions, err := s.transcriptRepository.QueryTranscriptSessions(
		ctx,
		transcript.SessionQuery{Limit: defaultTranscriptSessionLimit},
	)
	if err != nil {
		return TranscriptStatus{}, err
	}
	now := s.nowUTC()
	projected := make([]TranscriptSessionStatus, 0, len(sessions))
	for _, session := range sessions {
		lastActivity := session.EndedAt
		if lastActivity.IsZero() {
			lastActivity = session.StartedAt
		}
		age := now.Sub(lastActivity)
		recentlyActive := !lastActivity.IsZero() &&
			age >= 0 &&
			age <= activeTranscriptWindow
		active := recentlyActive &&
			(session.Coverage == transcript.CoverageLive ||
				session.EndedAt.IsZero())
		recent := !lastActivity.IsZero() &&
			age >= 0 &&
			age <= recentTranscriptWindow
		if !active && !recent {
			continue
		}
		var endedAt *time.Time
		if !session.EndedAt.IsZero() {
			value := session.EndedAt.UTC()
			endedAt = &value
		}
		projected = append(projected, TranscriptSessionStatus{
			SessionKey:     strings.TrimSpace(session.SessionKey),
			Agent:          strings.TrimSpace(session.Agent),
			Project:        transcriptProjectLabel(session),
			StartedAt:      session.StartedAt.UTC(),
			EndedAt:        endedAt,
			LastActivityAt: lastActivity.UTC(),
			Coverage:       session.Coverage,
			TurnCount:      session.TurnCount,
			Active:         active,
		})
	}
	sort.SliceStable(projected, func(left, right int) bool {
		if projected[left].Active != projected[right].Active {
			return projected[left].Active
		}
		return projected[left].LastActivityAt.After(
			projected[right].LastActivityAt,
		)
	})
	if len(projected) > defaultTranscriptSessionLimit {
		projected = projected[:defaultTranscriptSessionLimit]
	}
	return TranscriptStatus{
		SchemaVersion: TranscriptStatusSchemaVersion,
		Coverage: TranscriptCoverageStatus{
			WithTranscript: coverage.CanonicalCompleteOrLiveWithTranscript,
			Partial:        coverage.CanonicalPartial,
			Without:        coverage.CanonicalWithoutTranscript,
			TranscriptOnly: coverage.TranscriptOnly,
		},
		Sessions: nonNil(projected),
	}, nil
}

func transcriptProjectLabel(session transcript.Session) string {
	identity := strings.TrimSpace(session.ProjectIdentity)
	var label string
	if strings.TrimSpace(session.GitRemoteURL) != "" ||
		looksLikeRemote(identity) {
		label = remoteRepositoryBase(identity)
		if label == "" {
			label = remoteRepositoryBase(session.GitRemoteURL)
		}
	} else {
		label = localProjectBase(identity)
		if label == "" {
			label = localProjectBase(session.ProjectPath)
		}
	}
	label = strings.TrimSpace(label)
	if strings.HasSuffix(strings.ToLower(label), ".git") {
		label = label[:len(label)-len(".git")]
	}
	label = boundedSafeLabel(label)
	if label != "" {
		return label
	}
	return boundedSafeLabel(harnessProjectLabel(session.Agent))
}

func looksLikeRemote(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(lower, "://") ||
		strings.HasPrefix(lower, "git@") ||
		strings.Contains(lower, "@") && strings.Contains(lower, ":")
}

func remoteRepositoryBase(value string) string {
	value = strings.TrimSpace(strings.TrimRight(value, "/\\"))
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil &&
		parsed.Scheme != "" &&
		parsed.Path != "" {
		return path.Base(strings.TrimRight(parsed.Path, "/"))
	}
	if separator := strings.Index(value, ":"); separator >= 0 &&
		strings.Contains(value[:separator], "@") {
		value = value[separator+1:]
	}
	return path.Base(strings.ReplaceAll(value, "\\", "/"))
}

func localProjectBase(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	base := filepath.Base(filepath.Clean(value))
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

func boundedSafeLabel(value string) string {
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) ||
			character == '/' ||
			character == '\\' {
			return -1
		}
		return character
	}, value)
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > maxProjectLabelRunes {
		value = strings.TrimSpace(string(runes[:maxProjectLabelRunes]))
	}
	return value
}

func harnessProjectLabel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "claude", "claude-code":
		return "Claude Code"
	case "codex":
		return "Codex"
	default:
		return strings.TrimSpace(value)
	}
}
