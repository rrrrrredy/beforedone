package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rrrrrredy/beforedone/internal/repository"
)

type Result struct {
	SchemaVersion int      `json:"schema_version"`
	HooksPath     string   `json:"hooks_path"`
	AddedEvents   []string `json:"added_events"`
	RemovedEvents []string `json:"removed_events,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
}

func Codex(repo *repository.Repository) (*Result, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve BeforeDone executable: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute BeforeDone executable: %w", err)
	}
	unixCommand := shellQuote(executable) + " hook codex"
	windowsCommand := `"` + executable + `" hook codex`

	dir := filepath.Join(repo.Root, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "hooks.json")
	root := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if len(data) > 2<<20 {
			return nil, fmt.Errorf("existing hooks.json exceeds 2 MiB")
		}
		if err := json.Unmarshal(data, &root); err != nil {
			return nil, fmt.Errorf("parse existing hooks.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	hooksObject, ok := root["hooks"].(map[string]any)
	if !ok {
		if root["hooks"] != nil {
			return nil, fmt.Errorf("existing hooks field is not an object")
		}
		hooksObject = map[string]any{}
		root["hooks"] = hooksObject
	}
	result := &Result{SchemaVersion: 1, HooksPath: path}
	for _, event := range []string{"SessionStart", "SubagentStart", "PreToolUse", "PostToolUse", "UserPromptSubmit", "SubagentStop", "Stop"} {
		groups, err := asGroups(hooksObject[event])
		if err != nil {
			return nil, fmt.Errorf("existing %s hooks: %w", event, err)
		}
		if containsExactBeforeDone(groups, unixCommand, windowsCommand) {
			continue
		}
		groups, _ = stripBeforeDone(groups)
		timeout := 30
		statusMessage := "BeforeDone: capturing evidence"
		if event == "Stop" {
			timeout = 90
			statusMessage = "BeforeDone: checking fresh evidence"
		}
		group := map[string]any{
			"hooks": []any{map[string]any{
				"type":           "command",
				"command":        unixCommand,
				"commandWindows": windowsCommand,
				"timeout":        timeout,
				"statusMessage":  statusMessage,
			}},
		}
		groups = append(groups, group)
		hooksObject[event] = groups
		result.AddedEvents = append(result.AddedEvents, event)
	}
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, err
	}
	result.Warnings = append(result.Warnings, "review and trust the new hook definitions with `/hooks` in Codex")
	result.Warnings = append(result.Warnings, "project-local hooks are an alternative to the BeforeDone plugin; do not enable both or events and Stop checks will run twice")
	return result, nil
}

func RemoveCodex(repo *repository.Repository) (*Result, error) {
	path := filepath.Join(repo.Root, ".codex", "hooks.json")
	result := &Result{SchemaVersion: 1, HooksPath: path}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > 2<<20 {
		return nil, fmt.Errorf("existing hooks.json exceeds 2 MiB")
	}
	root := map[string]any{}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse existing hooks.json: %w", err)
	}
	hooksObject, ok := root["hooks"].(map[string]any)
	if !ok {
		if root["hooks"] == nil {
			return result, nil
		}
		return nil, fmt.Errorf("existing hooks field is not an object")
	}
	for event, value := range hooksObject {
		groups, err := asGroups(value)
		if err != nil {
			return nil, fmt.Errorf("existing %s hooks: %w", event, err)
		}
		kept, removed := stripBeforeDone(groups)
		if removed == 0 {
			continue
		}
		result.RemovedEvents = append(result.RemovedEvents, event)
		if len(kept) == 0 {
			delete(hooksObject, event)
		} else {
			hooksObject[event] = kept
		}
	}
	sort.Strings(result.RemovedEvents)
	updated, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	updated = append(updated, '\n')
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return nil, err
	}
	return result, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func asGroups(value any) ([]any, error) {
	if value == nil {
		return nil, nil
	}
	groups, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("expected an array")
	}
	return groups, nil
}

func containsExactBeforeDone(groups []any, unixCommand, windowsCommand string) bool {
	for _, rawGroup := range groups {
		group, ok := rawGroup.(map[string]any)
		if !ok {
			continue
		}
		handlers, _ := group["hooks"].([]any)
		for _, rawHandler := range handlers {
			handler, ok := rawHandler.(map[string]any)
			if !ok {
				continue
			}
			command, _ := handler["command"].(string)
			commandWindows, _ := handler["commandWindows"].(string)
			if commandWindows == "" {
				commandWindows, _ = handler["command_windows"].(string)
			}
			if command == unixCommand && commandWindows == windowsCommand {
				return true
			}
		}
	}
	return false
}

func stripBeforeDone(groups []any) ([]any, int) {
	result := make([]any, 0, len(groups))
	removed := 0
	for _, rawGroup := range groups {
		group, ok := rawGroup.(map[string]any)
		if !ok {
			result = append(result, rawGroup)
			continue
		}
		handlers, ok := group["hooks"].([]any)
		if !ok {
			result = append(result, rawGroup)
			continue
		}
		kept := make([]any, 0, len(handlers))
		for _, rawHandler := range handlers {
			handler, ok := rawHandler.(map[string]any)
			if !ok {
				kept = append(kept, rawHandler)
				continue
			}
			statusMessage, _ := handler["statusMessage"].(string)
			isBeforeDone := strings.HasPrefix(strings.ToLower(statusMessage), "beforedone")
			for _, key := range []string{"command", "commandWindows", "command_windows"} {
				if value, _ := handler[key].(string); strings.Contains(strings.ToLower(value), "beforedone") && strings.Contains(strings.ToLower(value), "hook codex") {
					isBeforeDone = true
				}
			}
			if !isBeforeDone {
				kept = append(kept, rawHandler)
			} else {
				removed++
			}
		}
		if len(kept) > 0 {
			group["hooks"] = kept
			result = append(result, group)
		}
	}
	return result, removed
}
