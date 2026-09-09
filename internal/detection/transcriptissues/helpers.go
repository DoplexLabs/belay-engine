package transcriptissues

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/issueintel"
	"github.com/DoplexLabs/belay-engine/internal/transcript"
)

func sessionRef(session preparedSession) issueintel.SessionRef {
	return issueintel.SessionRef{
		SessionKey: session.metadata.SessionKey,
		Agent:      session.metadata.Agent,
		StartedAt:  session.metadata.StartedAt,
		EndedAt:    session.metadata.EndedAt,
	}
}

func turnTokens(turn transcript.Turn) int64 {
	var result int64
	if turn.InputTokens != nil {
		result += *turn.InputTokens
	}
	if turn.OutputTokens != nil {
		result += *turn.OutputTokens
	}
	return result
}

func windowCost(
	turns []transcript.Turn,
	start, end int,
) issueintel.Cost {
	if start < 0 {
		start = 0
	}
	if end >= len(turns) {
		end = len(turns) - 1
	}
	if start > end || len(turns) == 0 {
		return issueintel.Cost{LowerBound: true}
	}
	result := issueintel.Cost{}
	usd := 0.0
	usdKnown := false
	for index := start; index <= end; index++ {
		result.WastedTokens += turnTokens(turns[index])
		if turns[index].CostUSD != nil {
			usd += *turns[index].CostUSD
			usdKnown = true
		} else if turnTokens(turns[index]) > 0 {
			result.LowerBound = true
		}
	}
	first := turns[start].OccurredAt
	last := turns[end].OccurredAt
	if !first.IsZero() && last.After(first) {
		result.WastedMinutes = last.Sub(first).Minutes()
	}
	if usdKnown {
		result.WastedUSD = &usd
	}
	return result
}

func excerptFromTurn(turn transcript.Turn) issueintel.Excerpt {
	text := strings.TrimSpace(turn.Payload.Text)
	if text == "" {
		text = strings.TrimSpace(turn.Payload.RawCommand)
	}
	if text == "" {
		text = strings.TrimSpace(turn.Payload.ToolResult)
	}
	if text == "" && len(turn.Payload.ToolInput) > 0 {
		var compact bytes.Buffer
		if json.Compact(&compact, turn.Payload.ToolInput) == nil {
			text = compact.String()
		} else {
			text = string(turn.Payload.ToolInput)
		}
	}
	return issueintel.Excerpt{
		Citation: issueintel.Citation{
			SessionKey:      turn.SessionKey,
			TurnIndex:       turn.TurnIndex,
			OccurredAt:      turn.OccurredAt,
			SourceFileID:    turn.Payload.SourceFileID,
			JSONLByteOffset: turn.Payload.JSONLByteOffset,
		},
		Role:     turn.Role,
		ToolName: turn.ToolName,
		Text:     text,
	}
}

func mergeCost(left, right issueintel.Cost) issueintel.Cost {
	result := issueintel.Cost{
		WastedMinutes: left.WastedMinutes + right.WastedMinutes,
		WastedTokens:  left.WastedTokens + right.WastedTokens,
		LowerBound:    left.LowerBound || right.LowerBound,
	}
	usd := 0.0
	known := false
	for _, value := range []*float64{left.WastedUSD, right.WastedUSD} {
		if value != nil {
			usd += *value
			known = true
		}
	}
	if known {
		result.WastedUSD = &usd
	}
	if left.WastedUSD == nil || right.WastedUSD == nil {
		result.LowerBound = true
	}
	return result
}

func observedBounds(turns ...transcript.Turn) (time.Time, time.Time) {
	var first, last time.Time
	for _, turn := range turns {
		if turn.OccurredAt.IsZero() {
			continue
		}
		if first.IsZero() || turn.OccurredAt.Before(first) {
			first = turn.OccurredAt
		}
		if last.IsZero() || turn.OccurredAt.After(last) {
			last = turn.OccurredAt
		}
	}
	return first, last
}

func instructionTarget(project preparedProject) string {
	claude, codex := 0, 0
	for _, session := range project.sessions {
		switch session.metadata.Agent {
		case "claude-code":
			claude++
		case "codex":
			codex++
		}
	}
	if claude > codex {
		return "CLAUDE.md"
	}
	return "AGENTS.md"
}
