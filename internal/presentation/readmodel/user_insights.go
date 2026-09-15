package readmodel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/issueintel"
	"github.com/DoplexLabs/belay-engine/internal/transcript"
	"github.com/DoplexLabs/belay-engine/internal/userinsights"
)

// UserInsightsProjectionVersion identifies the Habits read model. It is
// independent of every issue, report, and Mission Pack projection.
const UserInsightsProjectionVersion = "belay.user-insights.v1"

const (
	defaultUserInsightSessionLimit = 8
	maxUserInsightSessionLimit     = 25
	userInsightCandidateLimit      = 100
	userInsightTurnLimit           = 10000
	userInsightMinUserTurns        = 2
)

// ErrUserInsightsUnavailable is returned when Local has no transcript
// repository to read from.
var ErrUserInsightsUnavailable = errors.New("user insights unavailable")

// UserInsightRepository is the narrow transcript read surface the Habits view
// needs. The encrypted local store satisfies it.
type UserInsightRepository interface {
	QueryTranscriptSessions(
		context.Context,
		transcript.SessionQuery,
	) ([]transcript.Session, error)
	QueryTranscriptTurns(context.Context, string, int) ([]transcript.Turn, error)
}

// WithUserInsightRepository wires the Habits read model explicitly. Passing
// the transcript repository through WithTranscriptRepository also wires it
// when the repository exposes turn reads.
func WithUserInsightRepository(repository UserInsightRepository) Option {
	return func(service *Service) {
		if service != nil {
			service.userInsightRepository = repository
		}
	}
}

type UserInsightsRequest struct {
	Limit int
}

// UserInsightSession is one session debrief with a safe project label. It
// never carries transcript prose, raw commands, or file paths.
type UserInsightSession struct {
	userinsights.Retro
	Project string `json:"project"`
}

type UserInsightsCoverage struct {
	CandidateSessions int  `json:"candidate_sessions"`
	EvaluatedSessions int  `json:"evaluated_sessions"`
	SkippedIncomplete int  `json:"skipped_incomplete"`
	SkippedShort      int  `json:"skipped_short"`
	SkippedUnreadable int  `json:"skipped_unreadable"`
	TurnsTruncated    int  `json:"turns_truncated"`
	HasMore           bool `json:"has_more"`
}

type UserInsights struct {
	SchemaVersion     string               `json:"schema_version"`
	ProjectionVersion string               `json:"projection_version"`
	AnalysisVersion   string               `json:"analysis_version"`
	GeneratedAt       time.Time            `json:"generated_at"`
	Sessions          []UserInsightSession `json:"sessions"`
	Coverage          UserInsightsCoverage `json:"coverage"`
	Limitations       []string             `json:"limitations"`
}

// GetUserInsights builds debriefs for the developer's most recent complete
// sessions. Each debrief reads only that session's own turns, and each
// baseline reads only session metadata from the same project.
func (s *Service) GetUserInsights(
	ctx context.Context,
	request UserInsightsRequest,
) (UserInsights, error) {
	if s == nil || s.userInsightRepository == nil {
		return UserInsights{}, fmt.Errorf(
			"%w: transcript repository is required",
			ErrUserInsightsUnavailable,
		)
	}
	limit := request.Limit
	if limit <= 0 {
		limit = defaultUserInsightSessionLimit
	}
	if limit > maxUserInsightSessionLimit {
		return UserInsights{}, ErrInvalidRequest
	}
	candidates, err := s.userInsightRepository.QueryTranscriptSessions(
		ctx,
		transcript.SessionQuery{Limit: userInsightCandidateLimit},
	)
	if err != nil {
		return UserInsights{}, err
	}
	result := UserInsights{
		SchemaVersion:     SchemaVersion,
		ProjectionVersion: UserInsightsProjectionVersion,
		AnalysisVersion:   userinsights.AnalysisVersion,
		GeneratedAt:       s.nowUTC(),
		Sessions:          make([]UserInsightSession, 0, limit),
		Limitations:       []string{},
	}
	result.Coverage.CandidateSessions = len(candidates)
	result.Coverage.HasMore = len(candidates) >= userInsightCandidateLimit

	for _, session := range candidates {
		if len(result.Sessions) >= limit {
			result.Coverage.HasMore = true
			break
		}
		if session.Coverage != transcript.CoverageComplete {
			result.Coverage.SkippedIncomplete++
			continue
		}
		if session.UserTurnCount < userInsightMinUserTurns {
			result.Coverage.SkippedShort++
			continue
		}
		turns, err := s.userInsightRepository.QueryTranscriptTurns(
			ctx,
			session.SessionKey,
			userInsightTurnLimit,
		)
		if err != nil {
			if ctx.Err() != nil {
				return UserInsights{}, err
			}
			result.Coverage.SkippedUnreadable++
			continue
		}
		if len(turns) >= userInsightTurnLimit {
			result.Coverage.TurnsTruncated++
		}
		measurements := userinsights.Analyze(
			session,
			turns,
			issueintel.ProjectConfig{},
		)
		baseline := userinsights.ComputeBaseline(session, measurements, candidates)
		retro := userinsights.BuildRetro(session, measurements, baseline)
		result.Sessions = append(result.Sessions, UserInsightSession{
			Retro:   retro,
			Project: transcriptProjectLabel(session),
		})
		result.Coverage.EvaluatedSessions++
	}

	if result.Coverage.TurnsTruncated > 0 {
		result.Limitations = append(
			result.Limitations,
			"Some sessions were longer than Belay reads for a debrief, so their later turns were not counted.",
		)
	}
	if result.Coverage.SkippedIncomplete > 0 {
		result.Limitations = append(
			result.Limitations,
			"Live and partially captured sessions are skipped until they finish.",
		)
	}
	if len(result.Sessions) == 0 && len(candidates) == 0 {
		result.Limitations = append(
			result.Limitations,
			"No retained transcript sessions were found yet.",
		)
	}
	for index := range result.Sessions {
		result.Sessions[index].Project = strings.TrimSpace(result.Sessions[index].Project)
	}
	return result, nil
}
