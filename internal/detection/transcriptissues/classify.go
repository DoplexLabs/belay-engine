package transcriptissues

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/DoplexLabs/belay-engine/internal/issueintel"
	"github.com/DoplexLabs/belay-engine/internal/transcript"
)

const (
	commandClassTest      = "test"
	commandClassBuild     = "build"
	commandClassTypecheck = "typecheck"
	commandClassLint      = "lint"
	commandClassFormat    = "format_check"
	commandClassOther     = "other"
)

var (
	numberPattern       = regexp.MustCompile(`\b\d+(?:\.\d+)?\b`)
	hashPattern         = regexp.MustCompile(`\b[0-9a-f]{7,}\b`)
	portPattern         = regexp.MustCompile(`(?i)(?::|--port[=\s]+)\d{2,5}\b`)
	tempPathPattern     = regexp.MustCompile(`(?i)(?:/private)?/tmp/[^\s"'` + "`" + `]+|/var/folders/[^\s"'` + "`" + `]+`)
	absolutePathPattern = regexp.MustCompile(`(?:[A-Za-z]:\\|/)[^\s"'` + "`" + `:]+`)
	spacePattern        = regexp.MustCompile(`\s+`)
	errorMarkerPattern  = regexp.MustCompile(`(?i)\b(error|failed|failure|fatal|panic|exception|denied|not found|timed out)\b`)
	completionPattern   = regexp.MustCompile(`(?i)\b(done|complete|completed|implemented|finished|tests? pass(?:ed)?)\b`)
	correctionPattern   = regexp.MustCompile(`(?i)\b(no|don't|do not|stop|wrong|not that|i said|again|undo|revert)\b|(?i)\buse\b.+\bnot\b`)
	approvalPattern     = regexp.MustCompile(`(?i)^(yes|y|allow|allowed|approve|approved|continue|go ahead|ok|okay)$`)
	denialPattern       = regexp.MustCompile(`(?i)^(no|n|deny|denied|reject|rejected|cancel|stop)$`)
	patchFilePattern    = regexp.MustCompile(`(?m)^\*\*\* (?:Add|Update|Delete) File: (.+)$`)
)

func commandInfo(
	turn transcript.Turn,
	config issueintel.ProjectConfig,
) (class, normalized, raw string, ok bool) {
	raw = strings.TrimSpace(turn.Payload.RawCommand)
	if raw == "" && turn.Role == transcript.RoleToolCall {
		raw = commandFromToolInput(turn.Payload.ToolInput)
	}
	if raw == "" {
		return "", "", "", false
	}
	class = classifyCommand(raw, config)
	normalized = normalizeCommand(raw)
	if normalized == "" {
		return "", "", "", false
	}
	return class, normalized, raw, true
}

func commandFromToolInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return findStringField(value, map[string]bool{
		"command": true,
		"cmd":     true,
		"script":  true,
	})
}

func classifyCommand(raw string, config issueintel.ProjectConfig) string {
	normalized := strings.ToLower(spacePattern.ReplaceAllString(strings.TrimSpace(raw), " "))
	for _, configured := range config.VerificationCommands {
		configured = strings.ToLower(spacePattern.ReplaceAllString(strings.TrimSpace(configured), " "))
		if configured != "" && strings.HasPrefix(normalized, configured) {
			class := verificationClassFromWords(configured)
			if class == commandClassOther {
				return commandClassBuild
			}
			return class
		}
	}
	fields := strings.Fields(normalized)
	if len(fields) == 0 {
		return commandClassOther
	}
	executable := filepath.Base(fields[0])
	joined := " " + normalized + " "
	switch executable {
	case "go":
		if len(fields) > 1 && fields[1] == "test" {
			return commandClassTest
		}
		if len(fields) > 1 && fields[1] == "build" {
			return commandClassBuild
		}
	case "cargo":
		if len(fields) > 1 && fields[1] == "test" {
			return commandClassTest
		}
		if len(fields) > 1 && (fields[1] == "build" || fields[1] == "check") {
			return commandClassBuild
		}
	case "pytest", "jest", "vitest", "mocha":
		return commandClassTest
	case "tsc", "pyright", "mypy":
		return commandClassTypecheck
	case "eslint", "ruff", "golangci-lint", "shellcheck", "hadolint":
		return commandClassLint
	case "gofmt", "prettier", "black":
		if strings.Contains(joined, " --check ") ||
			strings.Contains(joined, " -d ") {
			return commandClassFormat
		}
	case "make":
		if len(fields) > 1 {
			return verificationClassFromWords(fields[1])
		}
	case "npm", "pnpm", "yarn", "bun":
		script := packageScript(fields)
		if script != "" {
			return verificationClassFromWords(script)
		}
	}
	return verificationClassFromWords(normalized)
}

func packageScript(fields []string) string {
	if len(fields) < 2 {
		return ""
	}
	index := 1
	if fields[index] == "run" {
		index++
	}
	if index >= len(fields) {
		return ""
	}
	return fields[index]
}

func verificationClassFromWords(value string) string {
	value = strings.ToLower(value)
	switch {
	case strings.Contains(value, "typecheck"),
		strings.Contains(value, "type-check"),
		strings.Contains(value, "check-types"):
		return commandClassTypecheck
	case strings.Contains(value, "lint"):
		return commandClassLint
	case strings.Contains(value, "format") && strings.Contains(value, "check"):
		return commandClassFormat
	case strings.Contains(value, "test"):
		return commandClassTest
	case strings.Contains(value, "build"),
		strings.Contains(value, "compile"),
		strings.TrimSpace(value) == "check":
		return commandClassBuild
	default:
		return commandClassOther
	}
}

func isVerificationTurn(turn transcript.Turn, config issueintel.ProjectConfig) bool {
	class, _, _, ok := commandInfo(turn, config)
	if !ok {
		return false
	}
	switch class {
	case commandClassTest,
		commandClassBuild,
		commandClassTypecheck,
		commandClassLint,
		commandClassFormat:
		return true
	default:
		return false
	}
}

func normalizeCommand(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = tempPathPattern.ReplaceAllString(value, "<tmp>")
	value = portPattern.ReplaceAllString(value, "<port>")
	value = hashPattern.ReplaceAllString(value, "<hash>")
	value = numberPattern.ReplaceAllString(value, "<n>")
	value = spacePattern.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

func commandSubject(normalized string) string {
	fields := strings.Fields(normalized)
	if len(fields) == 0 {
		return "The same command"
	}
	return "`" + filepath.Base(fields[0]) + "`"
}

func turnFailed(turn transcript.Turn) bool {
	if turn.Payload.ToolIsError != nil && *turn.Payload.ToolIsError {
		return true
	}
	if turn.Payload.ExitCode != nil && *turn.Payload.ExitCode != 0 {
		return true
	}
	return errorMarkerPattern.MatchString(firstMeaningfulLine(turn.Payload.ToolResult))
}

func normalizedErrorSignature(turn transcript.Turn) string {
	line := firstMeaningfulLine(turn.Payload.ToolResult)
	if line == "" || !errorMarkerPattern.MatchString(line) {
		return ""
	}
	line = strings.ToLower(line)
	line = tempPathPattern.ReplaceAllString(line, "<tmp>")
	line = absolutePathPattern.ReplaceAllString(line, "<path>")
	line = hashPattern.ReplaceAllString(line, "<hash>")
	line = numberPattern.ReplaceAllString(line, "<n>")
	line = spacePattern.ReplaceAllString(line, " ")
	return strings.TrimSpace(line)
}

func firstMeaningfulLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func isCompletionClaim(turn transcript.Turn) bool {
	return turn.Role == transcript.RoleAssistant &&
		completionPattern.MatchString(turn.Payload.Text)
}

func correctionMarker(value string) string {
	value = strings.TrimSpace(value)
	match := correctionPattern.FindString(value)
	return strings.ToLower(strings.TrimSpace(match))
}

func wordCount(value string) int {
	return len(strings.FieldsFunc(value, func(character rune) bool {
		return unicode.IsSpace(character) || unicode.IsPunct(character)
	}))
}

func isApprovalText(value string) bool {
	return approvalPattern.MatchString(strings.TrimSpace(value))
}

func isDenialText(value string) bool {
	return denialPattern.MatchString(strings.TrimSpace(value))
}

func editedFiles(turn transcript.Turn) []string {
	if turn.Role != transcript.RoleToolCall {
		return nil
	}
	name := strings.ToLower(strings.TrimSpace(turn.ToolName))
	if !strings.Contains(name, "edit") &&
		!strings.Contains(name, "write") &&
		!strings.Contains(name, "patch") {
		return nil
	}
	var files []string
	if len(turn.Payload.ToolInput) > 0 {
		var value any
		if json.Unmarshal(turn.Payload.ToolInput, &value) == nil {
			collectPathFields(value, &files)
			collectPatchPaths(value, &files)
		}
		for _, match := range patchFilePattern.FindAllStringSubmatch(
			string(turn.Payload.ToolInput),
			-1,
		) {
			if len(match) == 2 {
				files = append(files, match[1])
			}
		}
	}
	files = normalizeFiles(files)
	return files
}

func editContentHash(turn transcript.Turn) string {
	body := turn.Payload.ToolInput
	if len(body) == 0 {
		body = []byte(turn.Payload.RawCommand)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:16])
}

func findStringField(value any, names map[string]bool) string {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child := typed[key]
			if names[strings.ToLower(key)] {
				if text, ok := child.(string); ok {
					return text
				}
			}
			if found := findStringField(child, names); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range typed {
			if found := findStringField(child, names); found != "" {
				return found
			}
		}
	}
	return ""
}

func collectPathFields(value any, result *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			switch strings.ToLower(key) {
			case "path", "file", "file_path", "filename", "target_file":
				if text, ok := child.(string); ok {
					*result = append(*result, text)
				}
			}
			collectPathFields(child, result)
		}
	case []any:
		for _, child := range typed {
			collectPathFields(child, result)
		}
	}
}

func collectPatchPaths(value any, result *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			collectPatchPaths(child, result)
		}
	case []any:
		for _, child := range typed {
			collectPatchPaths(child, result)
		}
	case string:
		for _, match := range patchFilePattern.FindAllStringSubmatch(typed, -1) {
			if len(match) == 2 {
				*result = append(*result, match[1])
			}
		}
	}
}

func normalizeFiles(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = filepath.ToSlash(filepath.Clean(strings.TrimSpace(value)))
		if value == "" || value == "." || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func nearestPriorToolCall(turns []transcript.Turn, index int) (transcript.Turn, bool) {
	for prior := index - 1; prior >= 0 && index-prior <= 3; prior-- {
		if turns[prior].Role == transcript.RoleToolCall {
			return turns[prior], true
		}
		if turns[prior].Role == transcript.RoleUser {
			break
		}
	}
	return transcript.Turn{}, false
}

func candidateID(project string, turn transcript.Turn) string {
	sum := sha256.Sum256([]byte(
		project + "\x00" + turn.SessionKey + "\x00" + strconv.FormatInt(turn.TurnIndex, 10),
	))
	return "crc_" + hex.EncodeToString(sum[:16])
}
