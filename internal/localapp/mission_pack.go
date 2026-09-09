package localapp

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DoplexLabs/belay-engine/internal/detection/transcriptissues"
	"github.com/DoplexLabs/belay-engine/internal/issueintel"
	"github.com/DoplexLabs/belay-engine/internal/missionpack"
)

const (
	missionPackServiceRequestTimeout = 5 * time.Second
	missionPackGitTimeout            = time.Second
	missionPackGitOutputBytes        = 4 << 10
	maxMissionPackCWDBytes           = 4096
	maxMissionPackIssueBytes         = 512
	maxMissionPackTaskBytes          = 1120
	maxMissionPackTaskRunes          = 280
)

type MissionPackRepository interface {
	ResolveMissionPackProject(
		context.Context,
		missionpack.ProjectSelector,
	) (missionpack.ResolvedProject, error)
	ReadMissionPackEvidence(
		context.Context,
		string,
		missionpack.Limits,
	) (missionpack.EvidenceSnapshot, error)
}

type MissionPackService struct {
	repository MissionPackRepository
	now        func() time.Time
}

func NewMissionPackService(
	store MissionPackRepository,
) (*MissionPackService, error) {
	if store == nil {
		return nil, errors.New("Mission Pack service requires a store")
	}
	return &MissionPackService{
		repository: store,
		now:        time.Now,
	}, nil
}

func (s *MissionPackService) Generate(
	ctx context.Context,
	request missionpack.Request,
) (missionpack.Pack, error) {
	if ctx == nil {
		return missionpack.Pack{}, errors.New(
			"Mission Pack request requires context",
		)
	}
	request, err := validateMissionPackRequest(request)
	if err != nil {
		return missionpack.Pack{}, err
	}
	runCtx, cancel := context.WithTimeout(
		ctx,
		missionPackServiceRequestTimeout,
	)
	defer cancel()

	var issueProject *missionpack.ResolvedProject
	if request.IssueID != "" {
		resolved, err := s.repository.ResolveMissionPackProject(
			runCtx,
			missionpack.ProjectSelector{IssueID: request.IssueID},
		)
		if err != nil {
			return missionpack.Pack{}, err
		}
		issueProject = &resolved
	}

	var workspace missionPackWorkspace
	var cwdProject *missionpack.ResolvedProject
	if request.CWD != "" {
		workspace = observeMissionPackWorkspace(runCtx, request.CWD)
		resolved, err := s.repository.ResolveMissionPackProject(
			runCtx,
			missionpack.ProjectSelector{
				RemoteIdentity: workspace.remote,
				ProjectRoot:    workspace.root,
				ProjectPath:    workspace.path,
			},
		)
		if err != nil {
			return missionpack.Pack{}, err
		}
		cwdProject = &resolved
	}
	if issueProject != nil && cwdProject != nil &&
		issueProject.Identity != cwdProject.Identity {
		return missionpack.Pack{}, missionpack.ErrProjectMismatch
	}
	if err := runCtx.Err(); err != nil {
		return missionpack.Pack{}, err
	}

	resolved := issueProject
	if cwdProject != nil {
		resolved = cwdProject
	}
	if resolved == nil {
		return missionpack.Pack{}, errors.New(
			"Mission Pack request requires cwd or issue",
		)
	}
	if request.CWD == "" {
		if !filepath.IsAbs(resolved.Path) {
			return missionpack.Pack{}, missionpack.ErrProjectNotFound
		}
		workspace = observeMissionPackWorkspace(runCtx, resolved.Path)
		if workspace.remote != "" &&
			(resolved.IdentityKind != "remote" ||
				workspace.remote != resolved.Identity) {
			return missionpack.Pack{}, missionpack.ErrProjectNotFound
		}
	}
	if workspace.root == "" {
		workspace.root = workspace.path
	}
	configPath := workspace.root
	if !filepath.IsAbs(configPath) {
		configPath = resolved.Path
	}

	evidence, err := s.repository.ReadMissionPackEvidence(
		runCtx,
		resolved.Identity,
		missionpack.Limits{
			IssueID:      request.IssueID,
			Issues:       10,
			Sessions:     20,
			CommandTurns: 500,
			Events:       500,
		},
	)
	if err != nil {
		return missionpack.Pack{}, err
	}
	if err := runCtx.Err(); err != nil {
		return missionpack.Pack{}, err
	}
	projectFiles := discoverVerificationCommands(configPath)
	config := issueintel.ProjectConfig{
		VerificationCommands: make(
			[]string,
			0,
			len(projectFiles),
		),
	}
	for _, command := range projectFiles {
		config.VerificationCommands = append(
			config.VerificationCommands,
			command.Command,
		)
	}
	commands := observedMissionPackCommands(
		evidence.SuccessfulCommands,
		config,
	)
	resolved.Label = missionPackProjectLabel(
		resolved.Identity,
		configPath,
	)
	resolved.Path = configPath
	request.GeneratedAt = s.now().UTC()
	return missionpack.Build(missionpack.BuildInput{
		Request: request,
		Project: *resolved,
		Workspace: missionpack.WorkspaceSnapshot{
			Branch:    workspace.branch,
			Worktree:  missionPackWorktreeLabel(configPath),
			Harnesses: missionPackHarnesses(evidence.Sessions),
		},
		SourceState:  evidence.SourceState,
		Issues:       evidence.Issues,
		Insight:      evidence.Insight,
		Candidates:   evidence.Candidates,
		Commands:     commands,
		ProjectFiles: projectFiles,
		Facts:        evidence.Facts,
		Coverage:     evidence.Coverage,
		InsightStale: evidence.InsightStale,
	})
}

func validateMissionPackRequest(
	request missionpack.Request,
) (missionpack.Request, error) {
	request.CWD = strings.TrimSpace(request.CWD)
	request.IssueID = strings.TrimSpace(request.IssueID)
	request.Harness = missionpack.Harness(
		strings.TrimSpace(string(request.Harness)),
	)
	request.TaskHint = strings.TrimSpace(request.TaskHint)
	if request.CWD == "" && request.IssueID == "" {
		return missionpack.Request{}, errors.New(
			"Mission Pack request requires cwd or issue",
		)
	}
	if request.CWD != "" {
		if len(request.CWD) > maxMissionPackCWDBytes ||
			!filepath.IsAbs(request.CWD) {
			return missionpack.Request{}, errors.New(
				"Mission Pack cwd must be an absolute path",
			)
		}
		request.CWD = filepath.Clean(request.CWD)
	}
	if len(request.IssueID) > maxMissionPackIssueBytes {
		return missionpack.Request{}, errors.New(
			"Mission Pack issue ID is too long",
		)
	}
	if !utf8.ValidString(request.TaskHint) ||
		len(request.TaskHint) > maxMissionPackTaskBytes ||
		utf8.RuneCountInString(request.TaskHint) > maxMissionPackTaskRunes {
		return missionpack.Request{}, errors.New(
			"Mission Pack task hint is too long",
		)
	}
	if request.Intent == "" {
		request.Intent = missionpack.IntentGeneral
	}
	switch request.Intent {
	case missionpack.IntentGeneral,
		missionpack.IntentDebug,
		missionpack.IntentImplement,
		missionpack.IntentRefactor,
		missionpack.IntentReview,
		missionpack.IntentRelease:
	default:
		return missionpack.Request{}, errors.New(
			"invalid Mission Pack intent",
		)
	}
	switch request.Harness {
	case "", missionpack.HarnessClaude, missionpack.HarnessCodex:
	default:
		return missionpack.Request{}, errors.New(
			"invalid Mission Pack harness",
		)
	}
	return request, nil
}

type missionPackWorkspace struct {
	path      string
	root      string
	branch    string
	remote    string
	commonDir string
}

func observeMissionPackWorkspace(
	ctx context.Context,
	cwd string,
) missionPackWorkspace {
	result := missionPackWorkspace{path: filepath.Clean(cwd)}
	if resolved, err := filepath.EvalSymlinks(result.path); err == nil {
		result.path = filepath.Clean(resolved)
	}
	result.root, _ = runMissionPackGit(
		ctx,
		result.path,
		"rev-parse",
		"--show-toplevel",
	)
	if result.root == "" {
		return result
	}
	result.root = filepath.Clean(result.root)
	result.branch, _ = runMissionPackGit(
		ctx,
		result.root,
		"branch",
		"--show-current",
	)
	remote, _ := runMissionPackGit(
		ctx,
		result.root,
		"config",
		"--get",
		"remote.origin.url",
	)
	result.remote = sanitizeGitRemote(remote)
	result.commonDir, _ = runMissionPackGit(
		ctx,
		result.root,
		"rev-parse",
		"--git-common-dir",
	)
	return result
}

func runMissionPackGit(
	ctx context.Context,
	cwd string,
	args ...string,
) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, missionPackGitTimeout)
	defer cancel()
	var stdout limitedBuffer
	stdout.limit = missionPackGitOutputBytes
	commandArgs := append([]string{"-C", cwd}, args...)
	command := exec.CommandContext(commandCtx, "git", commandArgs...)
	command.Stdout = &stdout
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return "", err
	}
	if stdout.truncated {
		return "", errors.New("Git observation output exceeded limit")
	}
	return strings.TrimSpace(
		strings.ToValidUTF8(stdout.String(), "\uFFFD"),
	), nil
}

func observedMissionPackCommands(
	values []missionpack.SuccessfulCommand,
	config issueintel.ProjectConfig,
) []missionpack.ObservedCommand {
	type aggregate struct {
		command string
		class   string
		count   int
		last    time.Time
		sources []missionpack.SourceRef
	}
	merged := make(map[string]*aggregate)
	for _, value := range values {
		command := strings.TrimSpace(value.Command)
		if command == "" || utf8.RuneCountInString(command) > 256 ||
			strings.ContainsAny(command, "\r\n") ||
			!isCrispMissionPackObservedCommand(command) {
			continue
		}
		class, ok := transcriptissues.ClassifyVerificationCommand(
			command,
			config,
		)
		if !ok {
			continue
		}
		current := merged[command]
		if current == nil {
			current = &aggregate{command: command, class: class}
			merged[command] = current
		}
		current.count++
		if value.SucceededAt.After(current.last) {
			current.last = value.SucceededAt
		}
		if len(current.sources) < missionpack.MaxSourcesPerItem {
			current.sources = append(current.sources, value.Source)
		}
	}
	result := make([]missionpack.ObservedCommand, 0, len(merged))
	for _, value := range merged {
		result = append(result, missionpack.ObservedCommand{
			Command:      value.command,
			Class:        value.class,
			SuccessCount: value.count,
			LastSuccess:  value.last,
			Sources:      value.sources,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SuccessCount != result[j].SuccessCount {
			return result[i].SuccessCount > result[j].SuccessCount
		}
		if !result[i].LastSuccess.Equal(result[j].LastSuccess) {
			return result[i].LastSuccess.After(result[j].LastSuccess)
		}
		return result[i].Command < result[j].Command
	})
	return result
}

func isCrispMissionPackObservedCommand(command string) bool {
	return !strings.ContainsAny(command, ";|&<>`") &&
		!strings.Contains(command, "$(")
}

func missionPackHarnesses(
	sessions []missionpack.EvidenceSession,
) []string {
	set := make(map[string]bool)
	for _, session := range sessions {
		if harness := strings.TrimSpace(session.Harness); harness != "" {
			set[harness] = true
		}
	}
	result := make([]string, 0, len(set))
	for harness := range set {
		result = append(result, harness)
	}
	sort.Strings(result)
	return result
}

func missionPackProjectLabel(identity, projectPath string) string {
	value := strings.TrimSpace(identity)
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		value = parsed.Path
	} else if colon := strings.LastIndex(value, ":"); colon >= 0 &&
		!strings.Contains(value[colon+1:], "/") {
		value = value[colon+1:]
	}
	value = strings.TrimSuffix(strings.TrimRight(value, "/"), ".git")
	if base := filepath.Base(value); base != "." && base != "/" &&
		base != "" {
		return base
	}
	if base := filepath.Base(filepath.Clean(projectPath)); base != "." &&
		base != "/" && base != "" {
		return base
	}
	return "project"
}

func missionPackWorktreeLabel(projectPath string) string {
	if !filepath.IsAbs(projectPath) {
		return ""
	}
	value := filepath.Base(filepath.Clean(projectPath))
	if value == "." || value == "/" {
		return ""
	}
	return value
}
