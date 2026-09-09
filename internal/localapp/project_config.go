package localapp

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/DoplexLabs/belay-engine/internal/issueintel"
)

const maxProjectConfigBytes = 1 << 20

var (
	makeTargetPattern = regexp.MustCompile(`^([A-Za-z0-9_.-]+)\s*:(?:[^=]|$)`)
	tomlHeaderPattern = regexp.MustCompile(`^\s*\[([^\]]+)\]\s*$`)
	tomlKeyPattern    = regexp.MustCompile(`^\s*([A-Za-z0-9_.-]+)\s*=`)
)

func loadProjectConfig(projectPath string) issueintel.ProjectConfig {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return issueintel.ProjectConfig{}
	}
	result := issueintel.ProjectConfig{
		HasClaudeInstructions: regularProjectFile(
			filepath.Join(projectPath, "CLAUDE.md"),
		),
		HasCodexInstructions: regularProjectFile(
			filepath.Join(projectPath, "AGENTS.md"),
		),
	}
	commands := make(map[string]bool)
	loadPackageScripts(filepath.Join(projectPath, "package.json"), commands)
	loadMakeTargets(filepath.Join(projectPath, "Makefile"), commands)
	loadMakeTargets(filepath.Join(projectPath, "makefile"), commands)
	loadPyprojectCommands(filepath.Join(projectPath, "pyproject.toml"), commands)
	for command := range commands {
		result.VerificationCommands = append(result.VerificationCommands, command)
	}
	sort.Strings(result.VerificationCommands)
	return result
}

func loadPackageScripts(path string, result map[string]bool) {
	body, err := readBoundedProjectFile(path)
	if err != nil {
		return
	}
	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(body, &manifest) != nil {
		return
	}
	for name, body := range manifest.Scripts {
		if !looksLikeVerification(name + " " + body) {
			continue
		}
		for _, prefix := range []string{
			"npm run ",
			"pnpm run ",
			"yarn ",
			"bun run ",
		} {
			result[prefix+name] = true
		}
		if name == "test" {
			result["npm test"] = true
			result["pnpm test"] = true
			result["yarn test"] = true
			result["bun test"] = true
		}
	}
}

func loadMakeTargets(path string, result map[string]bool) {
	body, err := readBoundedProjectFile(path)
	if err != nil {
		return
	}
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		match := makeTargetPattern.FindStringSubmatch(scanner.Text())
		if len(match) != 2 || !looksLikeVerification(match[1]) {
			continue
		}
		result["make "+match[1]] = true
	}
}

func loadPyprojectCommands(path string, result map[string]bool) {
	body, err := readBoundedProjectFile(path)
	if err != nil {
		return
	}
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		line := scanner.Text()
		if match := tomlHeaderPattern.FindStringSubmatch(line); len(match) == 2 {
			section = strings.ToLower(match[1])
			switch {
			case strings.HasPrefix(section, "tool.pytest"):
				result["pytest"] = true
			case strings.HasPrefix(section, "tool.ruff"):
				result["ruff check"] = true
			case strings.HasPrefix(section, "tool.mypy"):
				result["mypy"] = true
			case strings.HasPrefix(section, "tool.pyright"):
				result["pyright"] = true
			case strings.HasPrefix(section, "tool.black"):
				result["black --check"] = true
			}
			continue
		}
		match := tomlKeyPattern.FindStringSubmatch(line)
		if len(match) != 2 || !looksLikeVerification(match[1]) {
			continue
		}
		switch {
		case strings.Contains(section, "poe.tasks"):
			result["poe "+match[1]] = true
		case strings.Contains(section, ".scripts"):
			result["hatch run "+match[1]] = true
		}
	}
}

func looksLikeVerification(value string) bool {
	value = strings.ToLower(value)
	for _, marker := range []string{
		"test",
		"check",
		"verify",
		"lint",
		"type",
		"build",
		"compile",
		"format",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func regularProjectFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil &&
		info.Mode().IsRegular() &&
		info.Mode()&os.ModeSymlink == 0
}

func readBoundedProjectFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		if err != nil {
			return nil, err
		}
		return nil, os.ErrInvalid
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := io.LimitReader(file, maxProjectConfigBytes+1)
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if len(body) > maxProjectConfigBytes {
		return nil, os.ErrInvalid
	}
	return body, nil
}
