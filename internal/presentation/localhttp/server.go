package localhttp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/DoplexLabs/belay-engine/internal/canonical/model"
	"github.com/DoplexLabs/belay-engine/internal/presentation/readmodel"
)

//go:embed assets/*
var assetFiles embed.FS

type Server struct {
	read  *readmodel.Service
	token string
}

type RunningServer struct {
	URL   string
	Token string
	done  chan error
	http  *http.Server
}

type problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail"`
	RequestID string `json:"request_id"`
}

func New(read *readmodel.Service, token string) (*Server, error) {
	if read == nil {
		return nil, errors.New("local HTTP server requires a read service")
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("local HTTP server requires a launch token")
	}
	return &Server{read: read, token: token}, nil
}

func NewLaunchToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate local launch token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("GET /v1/sessions", s.authorize(http.HandlerFunc(s.listSessions)))
	mux.Handle("GET /v1/sessions/{id}", s.authorize(http.HandlerFunc(s.getSession)))
	mux.Handle("GET /v1/sessions/{id}/events", s.authorize(http.HandlerFunc(s.getTimeline)))
	mux.Handle("GET /v1/activity", s.authorize(http.HandlerFunc(s.queryActivity)))
	mux.Handle("GET /v1/findings", s.authorize(http.HandlerFunc(s.listFindings)))
	mux.Handle("GET /v1/stats", s.authorize(http.HandlerFunc(s.getStats)))

	assets, err := fs.Sub(assetFiles, "assets")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", http.FileServerFS(assets))
	return securityHeaders(loopbackOnly(mux))
}

func (s *Server) Start(ctx context.Context, address string) (*RunningServer, error) {
	if address == "" {
		address = "127.0.0.1:0"
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("parse Local listen address: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("Belay Local may bind only to a loopback address")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen for Belay Local: %w", err)
	}
	server := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	running := &RunningServer{
		URL:   "http://" + listener.Addr().String(),
		Token: s.token,
		done:  make(chan error, 1),
		http:  server,
	}
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		running.done <- err
		close(running.done)
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	return running, nil
}

func (r *RunningServer) BrowserURL() string {
	return r.URL + "/#token=" + url.QueryEscape(r.Token)
}

func (r *RunningServer) Wait() error {
	return <-r.done
}

func (r *RunningServer) Close(ctx context.Context) error {
	return r.http.Shutdown(ctx)
}

func (s *Server) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, prefix) ||
			subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(header, prefix)), []byte(s.token)) != 1 {
			writeProblem(w, r, http.StatusUnauthorized, "Unauthorized", "A valid per-launch Local token is required.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	response, err := s.read.ListSessions(r.Context(), boundedInt(r, "limit", 20, 100))
	writeReadResult(w, r, response, err)
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	response, err := s.read.GetSession(r.Context(), r.PathValue("id"))
	writeReadResult(w, r, response, err)
}

func (s *Server) getTimeline(w http.ResponseWriter, r *http.Request) {
	response, err := s.read.GetSessionTimeline(
		r.Context(),
		r.PathValue("id"),
		boundedInt(r, "limit", 100, 500),
	)
	writeReadResult(w, r, response, err)
}

func (s *Server) queryActivity(w http.ResponseWriter, r *http.Request) {
	filter := model.ActivityFilter{
		Harness:      boundedQuery(r, "harness", 128),
		ResourceKind: boundedQuery(r, "resource_kind", 64),
		Outcome:      boundedQuery(r, "outcome", 32),
		Limit:        boundedInt(r, "limit", 50, 200),
	}
	var err error
	filter.OccurredAfter, err = optionalTime(r, "occurred_after")
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "Invalid request", "occurred_after must be an RFC3339 timestamp.")
		return
	}
	filter.OccurredBefore, err = optionalTime(r, "occurred_before")
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "Invalid request", "occurred_before must be an RFC3339 timestamp.")
		return
	}
	response, err := s.read.QueryActivity(r.Context(), filter)
	writeReadResult(w, r, response, err)
}

func (s *Server) listFindings(w http.ResponseWriter, r *http.Request) {
	response, err := s.read.ListFindings(r.Context(), boundedInt(r, "limit", 20, 100))
	writeReadResult(w, r, response, err)
}

func (s *Server) getStats(w http.ResponseWriter, r *http.Request) {
	response, err := s.read.GetStats(r.Context())
	writeReadResult(w, r, response, err)
}

func writeReadResult(w http.ResponseWriter, r *http.Request, response any, err error) {
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "Local read failed", "Belay could not complete the local read.")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			writeProblem(w, r, http.StatusForbidden, "Forbidden", "Belay Local accepts loopback requests only.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func boundedInt(r *http.Request, name string, fallback, maximum int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil || value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func boundedQuery(r *http.Request, name string, maximum int) string {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if len(value) > maximum {
		value = value[:maximum]
	}
	return value
}

func optionalTime(r *http.Request, name string) (*time.Time, error) {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, err
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{
		Type:      "about:blank",
		Title:     title,
		Status:    status,
		Detail:    detail,
		RequestID: requestID(r),
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func requestID(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-Request-ID")); value != "" && len(value) <= 128 {
		return value
	}
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "local-request"
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
