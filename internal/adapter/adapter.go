package adapter

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rrrrrredy/beforedone/internal/hooks"
	"github.com/rrrrrredy/beforedone/internal/model"
	"github.com/rrrrrredy/beforedone/internal/repository"
)

const maxInput = 16 << 20

type IngestResult struct {
	SchemaVersion int      `json:"schema_version"`
	Ingested      int      `json:"ingested"`
	EventIDs      []string `json:"event_ids"`
}

type TestResult struct {
	SchemaVersion int      `json:"schema_version"`
	Files         int      `json:"files"`
	Cases         int      `json:"cases"`
	Passed        int      `json:"passed"`
	Failures      []string `json:"failures,omitempty"`
}

func Ingest(repo *repository.Repository, cfg model.Config, reader io.Reader) (*IngestResult, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxInput+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxInput {
		return nil, errors.New("adapter input exceeds 16 MiB")
	}
	objects, err := decodeObjects(data)
	if err != nil {
		return nil, err
	}
	result := &IngestResult{SchemaVersion: model.SchemaVersion}
	events := make([]model.Event, 0, len(objects))
	for _, object := range objects {
		event, err := normalizeObject(object, cfg)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
		result.EventIDs = append(result.EventIDs, event.ID)
	}
	if err := hooks.AppendEvents(repo, events); err != nil {
		return nil, err
	}
	result.Ingested = len(events)
	return result, nil
}

func Test(path string, cfg model.Config) (*TestResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	var files []string
	if info.IsDir() {
		err = filepath.WalkDir(path, func(file string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".json") || strings.HasSuffix(entry.Name(), ".jsonl")) {
				files = append(files, file)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		files = []string{path}
	}
	sort.Strings(files)
	result := &TestResult{SchemaVersion: model.SchemaVersion, Files: len(files)}
	for _, file := range files {
		data, err := readInputFile(file)
		if err != nil {
			result.Failures = append(result.Failures, file+": "+err.Error())
			continue
		}
		if strings.Contains(strings.ToLower(filepath.Base(file)), "manifest") {
			result.Cases++
			if err := validateManifest(data); err != nil {
				result.Failures = append(result.Failures, file+": "+err.Error())
			} else {
				result.Passed++
			}
			continue
		}
		objects, err := decodeObjects(data)
		if err != nil {
			result.Failures = append(result.Failures, file+": "+err.Error())
			continue
		}
		for index, object := range objects {
			result.Cases++
			if err := testObject(object, cfg); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("%s[%d]: %v", file, index, err))
			} else {
				result.Passed++
			}
		}
	}
	return result, nil
}

func testObject(object map[string]any, cfg model.Config) error {
	if rawInput, ok := object["input"].(map[string]any); ok {
		event, err := normalizeObject(rawInput, cfg)
		if err != nil {
			return err
		}
		expected, ok := object["expected"].(map[string]any)
		if !ok {
			return errors.New("fixture expected object is missing")
		}
		if want, _ := expected["type"].(string); want != "" && string(event.Type) != want {
			return fmt.Errorf("event type = %s, want %s", event.Type, want)
		}
		if want, ok := numberToInt(expected["exit_code"]); ok {
			if event.ExitCode == nil || *event.ExitCode != want {
				return fmt.Errorf("exit_code does not equal %d", want)
			}
		}
		return nil
	}
	_, err := normalizeObject(object, cfg)
	return err
}

func normalizeObject(object map[string]any, cfg model.Config) (model.Event, error) {
	if _, ok := object["hook_event_name"]; ok {
		return hooks.NormalizeCodex(object, cfg)
	}
	data, err := json.Marshal(object)
	if err != nil {
		return model.Event{}, err
	}
	var event model.Event
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&event); err != nil {
		return model.Event{}, err
	}
	if err := hooks.ValidateEvent(event); err != nil {
		return model.Event{}, err
	}
	event = hooks.SanitizeEvent(event, cfg)
	if err := hooks.ValidateEvent(event); err != nil {
		return model.Event{}, fmt.Errorf("normalized event invalid after sanitization: %w", err)
	}
	return event, nil
}

func readInputFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil {
		return nil, err
	} else if info.Size() > maxInput {
		return nil, errors.New("input exceeds 16 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxInput+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxInput {
		return nil, errors.New("input exceeds 16 MiB")
	}
	return data, nil
}

func decodeObjects(data []byte) ([]map[string]any, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, errors.New("adapter input is empty")
	}
	decode := func(raw []byte) (map[string]any, error) {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var object map[string]any
		if err := dec.Decode(&object); err != nil {
			return nil, err
		}
		var trailing any
		if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
			if err == nil {
				return nil, errors.New("adapter object contains trailing JSON")
			}
			return nil, err
		}
		return object, nil
	}
	if data[0] == '[' {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		var objects []map[string]any
		if err := dec.Decode(&objects); err != nil {
			return nil, err
		}
		var trailing any
		if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
			if err == nil {
				return nil, errors.New("adapter array contains trailing JSON")
			}
			return nil, err
		}
		return objects, nil
	}
	if data[0] == '{' {
		object, err := decode(data)
		if err == nil {
			return []map[string]any{object}, nil
		}
	}
	var objects []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 2<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		object, err := decode(line)
		if err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(objects) == 0 {
		return nil, errors.New("adapter input contains no events")
	}
	return objects, nil
}

func validateManifest(data []byte) error {
	var manifest model.AdapterManifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&manifest); err != nil {
		return err
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("adapter manifest contains trailing JSON")
		}
		return err
	}
	if manifest.SchemaVersion != model.SchemaVersion || manifest.Name == "" || manifest.Version == "" {
		return errors.New("invalid adapter manifest contract")
	}
	allowed := map[string]bool{"tool_events": true, "stop_retry": true, "subagents": true, "transcript": true}
	for capability := range allowed {
		if _, ok := manifest.Capabilities[capability]; !ok {
			return fmt.Errorf("missing capability %q", capability)
		}
	}
	for capability := range manifest.Capabilities {
		if !allowed[capability] {
			return fmt.Errorf("unknown capability %q", capability)
		}
	}
	for source, target := range manifest.EventMap {
		if strings.TrimSpace(source) == "" || !model.EventType(target).Valid() {
			return fmt.Errorf("invalid event mapping %q -> %q", source, target)
		}
	}
	return nil
}

func numberToInt(value any) (int, bool) {
	switch v := value.(type) {
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	case float64:
		return int(v), true
	case int:
		return v, true
	default:
		return 0, false
	}
}
