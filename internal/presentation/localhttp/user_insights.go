package localhttp

import (
	"errors"
	"net/http"

	"github.com/DoplexLabs/belay-engine/internal/presentation/readmodel"
)

const maxUserInsightsLimit = 25

// getUserInsights serves the Habits view. It accepts only an optional
// bounded limit and shares no state with the issue, report, or Mission Pack
// handlers.
func (s *Server) getUserInsights(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	for name := range query {
		if name != "limit" {
			writeReadInvalidRequest(w, r)
			return
		}
	}
	limit := 0
	if raw := queryValue(r, "limit"); raw != "" {
		parsed := boundedInt(r, "limit", -1, maxUserInsightsLimit)
		if parsed <= 0 {
			writeReadInvalidRequest(w, r)
			return
		}
		limit = parsed
	}
	response, err := s.read.GetUserInsights(
		r.Context(),
		readmodel.UserInsightsRequest{Limit: limit},
	)
	if errors.Is(err, readmodel.ErrUserInsightsUnavailable) {
		writeProblemType(
			w,
			r,
			http.StatusServiceUnavailable,
			"belay.local/user-insights-unavailable",
			"Habits unavailable",
			"Belay could not read retained transcript sessions for a debrief.",
		)
		return
	}
	writeReadResult(w, r, response, err)
}
