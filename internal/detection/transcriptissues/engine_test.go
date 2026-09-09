package transcriptissues

import (
	"testing"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/issueintel"
	"github.com/DoplexLabs/belay-engine/internal/transcript"
)

func TestAggregateIssuesRanksMeasuredUSDThenSessionCount(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	knownUSD := 2.5
	project := preparedProject{
		project: issueintel.Project{Identity: "project", Path: "/project"},
		now:     now,
	}
	observations := []observation{
		{
			detectorID:  issueintel.DetectorRetryLoop,
			fingerprint: "known",
			subject:     "`go`",
			cost: issueintel.Cost{
				WastedTokens: 100,
				WastedUSD:    &knownUSD,
			},
			session: issueintel.SessionRef{
				SessionKey: "ses_known",
				StartedAt:  now.Add(-time.Hour),
			},
			firstSeen:  now.Add(-time.Hour),
			lastSeen:   now,
			occurredAt: now,
			excerpts: []issueintel.Excerpt{
				{
					Citation: issueintel.Citation{
						SessionKey: "ses_known",
						TurnIndex:  1,
					},
					Role: transcript.RoleToolCall,
					Text: "go test ./...",
				},
				{
					Citation: issueintel.Citation{
						SessionKey: "ses_known",
						TurnIndex:  2,
					},
					Role: transcript.RoleToolResult,
					Text: "error: known",
				},
			},
		},
	}
	for index := 0; index < 3; index++ {
		sessionKey := "ses_unknown_" + string(rune('a'+index))
		observations = append(observations, observation{
			detectorID:  issueintel.DetectorRecurringError,
			fingerprint: "unknown",
			subject:     "The same error",
			cost: issueintel.Cost{
				WastedTokens: 1000,
				LowerBound:   true,
			},
			session: issueintel.SessionRef{
				SessionKey: sessionKey,
				StartedAt:  now.Add(-time.Duration(index+2) * time.Hour),
			},
			firstSeen:  now.Add(-time.Duration(index+2) * time.Hour),
			lastSeen:   now,
			occurredAt: now,
			excerpts: []issueintel.Excerpt{{
				Citation: issueintel.Citation{SessionKey: sessionKey},
				Role:     transcript.RoleToolResult,
				Text:     "error: unknown",
			}},
		})
	}
	issues := aggregateIssues(project, observations)
	if len(issues) != 2 ||
		issues[0].Fingerprint != "known" ||
		issues[1].Fingerprint != "unknown" {
		t.Fatalf("issue order = %+v", issues)
	}
	if len(issues[0].Trend) != 8 || issues[1].SessionCount != 3 {
		t.Fatalf("aggregated issues = %+v", issues)
	}
}

func TestAggregateIssuesSumsCostBoundsAndDeduplicatesExcerpts(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	firstUSD, secondUSD := 1.25, 0.75
	project := preparedProject{
		project: issueintel.Project{Identity: "project", Path: "/project"},
		now:     now,
	}
	makeObservation := func(
		session string,
		occurred time.Time,
		usd *float64,
		lowerBound bool,
	) observation {
		return observation{
			detectorID:  issueintel.DetectorFileThrash,
			fingerprint: "file.go",
			subject:     "`file.go`",
			cost: issueintel.Cost{
				WastedMinutes: 2,
				WastedTokens:  50,
				WastedUSD:     usd,
				LowerBound:    lowerBound,
			},
			session:    issueintel.SessionRef{SessionKey: session, StartedAt: occurred},
			firstSeen:  occurred,
			lastSeen:   occurred,
			occurredAt: occurred,
			excerpts: []issueintel.Excerpt{
				{
					Citation: issueintel.Citation{
						SessionKey: session,
						TurnIndex:  1,
					},
					Role: transcript.RoleToolCall,
					Text: "edit file.go",
				},
				{
					Citation: issueintel.Citation{
						SessionKey: session,
						TurnIndex:  1,
					},
					Role: transcript.RoleToolCall,
					Text: "edit file.go",
				},
			},
		}
	}
	issues := aggregateIssues(project, []observation{
		makeObservation("ses_a", now.Add(-8*24*time.Hour), &firstUSD, false),
		makeObservation("ses_b", now, &secondUSD, true),
	})
	if len(issues) != 1 {
		t.Fatalf("issues = %+v", issues)
	}
	issue := issues[0]
	if issue.Cost.WastedUSD == nil ||
		*issue.Cost.WastedUSD != 2 ||
		issue.Cost.WastedTokens != 100 ||
		!issue.Cost.LowerBound ||
		len(issue.Excerpts) != 2 ||
		issue.Trend[6].Count != 1 ||
		issue.Trend[7].Count != 1 {
		t.Fatalf("issue = %+v", issue)
	}
}
