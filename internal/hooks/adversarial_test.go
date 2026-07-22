package hooks

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rrrrrredy/beforedone/internal/checker"
	"github.com/rrrrrredy/beforedone/internal/evidence"
	"github.com/rrrrrredy/beforedone/internal/model"
	"github.com/rrrrrredy/beforedone/internal/repository"
	"gopkg.in/yaml.v3"
)

func TestStopBlocksOnlyFirstAttemptWhenEvidenceIsMissing(t *testing.T) {
	repo, cfg := newHookTestRepo(t, []string{"git", "status", "--short"})
	_ = cfg

	first := runStopHook(t, repo.Root, false)
	if first.Decision != "block" {
		t.Fatalf("first Stop = %+v, want one blocking continuation", first)
	}
	if !strings.Contains(first.Reason, "beforedone check unit") {
		t.Fatalf("block reason is not actionable: %q", first.Reason)
	}

	second := runStopHook(t, repo.Root, true)
	if second.Decision == "block" {
		t.Fatalf("second Stop = %+v, want non-blocking loop guard", second)
	}
}

func TestAppendEventRejectsUnreadableRecordBeforePersistence(t *testing.T) {
	repo, _ := newHookTestRepo(t, []string{"git", "status", "--short"})
	attributes := make(map[string]string, MaxEventAttributes+1)
	for i := 0; i <= MaxEventAttributes; i++ {
		attributes[fmt.Sprintf("field-%03d", i)] = "safe"
	}
	event := model.Event{
		SchemaVersion: model.SchemaVersion,
		ID:            "oversized-event",
		OccurredAt:    time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC),
		Type:          model.ToolFinished,
		Source:        "fixture",
		Attributes:    attributes,
	}
	if err := AppendEvent(repo, event); err == nil || !strings.Contains(err.Error(), "exceeds 256 attributes") {
		t.Fatalf("AppendEvent error = %v, want bounded attribute rejection", err)
	}
	if _, err := os.Stat(filepath.Join(repo.RuntimeDir, "events.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("rejected event created a ledger: %v", err)
	}
}

func TestSanitizeEventProducesValidByteBoundedUTF8(t *testing.T) {
	event := model.Event{
		SchemaVersion: model.SchemaVersion,
		ID:            "bounded-sanitize",
		OccurredAt:    time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		Type:          model.ToolFinished,
		Source:        strings.Repeat("é", 40),
		Attributes:    map[string]string{"output": strings.Repeat("界", 400)},
	}
	event = SanitizeEvent(event, model.Config{})
	if !utf8.ValidString(event.Source) || !utf8.ValidString(event.Attributes["output"]) {
		t.Fatalf("sanitizer split UTF-8: source=%q output=%q", event.Source, event.Attributes["output"])
	}
	if len(event.Source) > 64 || len(event.Attributes["output"]) > 1024 {
		t.Fatalf("sanitizer exceeded byte limits: source=%d output=%d", len(event.Source), len(event.Attributes["output"]))
	}
	if err := ValidateEvent(event); err != nil {
		t.Fatalf("sanitized event did not satisfy its durable contract: %v", err)
	}
}

func TestAppendEventsRejectsDuplicateIDsAcrossConcurrentWriters(t *testing.T) {
	repo, _ := newHookTestRepo(t, []string{"git", "status", "--short"})
	event := model.Event{
		SchemaVersion: model.SchemaVersion,
		ID:            "same-event-id",
		OccurredAt:    time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC),
		Type:          model.SessionStarted,
		Source:        "fixture",
	}
	errors := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			errors <- AppendEvent(repo, event)
		}()
	}
	close(start)
	var successes, duplicates int
	for range 2 {
		err := <-errors
		switch {
		case err == nil:
			successes++
		case strings.Contains(err.Error(), "duplicate normalized event id"):
			duplicates++
		default:
			t.Fatalf("unexpected concurrent append error: %v", err)
		}
	}
	if successes != 1 || duplicates != 1 {
		t.Fatalf("successes=%d duplicates=%d", successes, duplicates)
	}
	events, err := ReadEvents(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != event.ID {
		t.Fatalf("concurrent duplicate ledger = %+v", events)
	}
}

func TestAppendFailsClosedWhenCommittedIDClaimIsMissing(t *testing.T) {
	repo, _ := newHookTestRepo(t, []string{"git", "status", "--short"})
	event := model.Event{
		SchemaVersion: model.SchemaVersion,
		ID:            "claim-must-remain",
		OccurredAt:    time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		Type:          model.SessionStarted,
		Source:        "fixture",
	}
	if err := AppendEvent(repo, event); err != nil {
		t.Fatal(err)
	}
	store, err := ensureEventStoreDirs(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	if err := os.Remove(eventIDClaimPath(store, event.ID)); err != nil {
		t.Fatal(err)
	}
	next := event
	next.ID = "must-not-append-with-incomplete-index"
	if err := AppendEvent(repo, next); err == nil || !strings.Contains(err.Error(), "ID index") {
		t.Fatalf("AppendEvent error = %v, want missing-claim fail-closed", err)
	}
}

func TestAppendFailsClosedWhenCommittedIDClaimIsSubstituted(t *testing.T) {
	repo, _ := newHookTestRepo(t, []string{"git", "status", "--short"})
	event := model.Event{
		SchemaVersion: model.SchemaVersion,
		ID:            "claim-cannot-be-substituted",
		OccurredAt:    time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		Type:          model.SessionStarted,
		Source:        "fixture",
	}
	if err := AppendEvent(repo, event); err != nil {
		t.Fatal(err)
	}
	store, err := ensureEventStoreDirs(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	original := eventIDClaimPath(store, event.ID)
	substitute := filepath.Join(store.ids, strings.Repeat("a", 64)+".json")
	if original == substitute {
		substitute = filepath.Join(store.ids, strings.Repeat("b", 64)+".json")
	}
	if err := os.Rename(original, substitute); err != nil {
		t.Fatal(err)
	}
	next := event
	next.ID = "must-not-append-with-substituted-index"
	if err := AppendEvent(repo, next); err == nil || !strings.Contains(err.Error(), "index does not match") {
		t.Fatalf("AppendEvent error = %v, want substituted-claim fail-closed", err)
	}
}

func TestAppendFailsClosedOnHistoricalSegmentCorruption(t *testing.T) {
	repo, _ := newHookTestRepo(t, []string{"git", "status", "--short"})
	event := model.Event{
		SchemaVersion: model.SchemaVersion,
		ID:            "historical-segment",
		OccurredAt:    time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		Type:          model.SessionStarted,
		Source:        "fixture",
	}
	if err := AppendEvent(repo, event); err != nil {
		t.Fatal(err)
	}
	store, err := ensureEventStoreDirs(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	segments, err := listEventSegments(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 1 {
		t.Fatalf("segments=%d, want 1", len(segments))
	}
	raw, err := os.ReadFile(segments[0].path)
	if err != nil {
		t.Fatal(err)
	}
	corrupted := bytes.Replace(raw, []byte(`"source":"fixture"`), []byte(`"source":"fixturf"`), 1)
	if bytes.Equal(raw, corrupted) || len(raw) != len(corrupted) {
		t.Fatal("could not create same-length segment corruption")
	}
	if err := os.WriteFile(segments[0].path, corrupted, 0o600); err != nil {
		t.Fatal(err)
	}
	next := event
	next.ID = "must-not-extend-corrupt-ledger"
	if err := AppendEvent(repo, next); err == nil || !strings.Contains(err.Error(), "immutable metadata") {
		t.Fatalf("AppendEvent error = %v, want historical corruption rejection", err)
	}
}

func TestAppendRecomputesStateFromImmutableSegmentMetadata(t *testing.T) {
	repo, _ := newHookTestRepo(t, []string{"git", "status", "--short"})
	first := model.Event{
		SchemaVersion: model.SchemaVersion,
		ID:            "state-source-of-truth",
		OccurredAt:    time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		Type:          model.SessionStarted,
		Source:        "fixture",
	}
	if err := AppendEvent(repo, first); err != nil {
		t.Fatal(err)
	}
	store, err := ensureEventStoreDirs(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	state, _, err := loadEventStoreState(store)
	if err != nil {
		t.Fatal(err)
	}
	state.TotalBytes = 0
	state.EventCount = 0
	if err := saveEventStoreState(store, state); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID = "state-cache-repaired"
	if err := AppendEvent(repo, second); err != nil {
		t.Fatalf("append after stale state cache failed: %v", err)
	}
	repaired, _, err := loadEventStoreState(store)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.EventCount != 2 || repaired.TotalBytes <= 0 {
		t.Fatalf("state cache was not reconstructed: %+v", repaired)
	}
}

func TestLegacyWriterAfterMigrationFailsClosed(t *testing.T) {
	repo, _ := newHookTestRepo(t, []string{"git", "status", "--short"})
	if err := repo.EnsureRuntime(); err != nil {
		t.Fatal(err)
	}
	legacy := model.Event{
		SchemaVersion: model.SchemaVersion,
		ID:            "legacy-before-migration",
		OccurredAt:    time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		Type:          model.SessionStarted,
		Source:        "legacy",
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(repo.RuntimeDir, "events.jsonl")
	if err := os.WriteFile(legacyPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	current := legacy
	current.ID = "current-after-migration"
	if err := AppendEvent(repo, current); err != nil {
		t.Fatal(err)
	}
	oldWriter := legacy
	oldWriter.ID = "old-writer-after-migration"
	oldRaw, err := json.Marshal(oldWriter)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(legacyPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(append(oldRaw, '\n')); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEvents(repo); err == nil || !strings.Contains(err.Error(), "does not match the migrated first segment") {
		t.Fatalf("ReadEvents error = %v, want legacy-writer detection", err)
	}
	current.ID = "must-not-ignore-old-writer"
	if err := AppendEvent(repo, current); err == nil || !strings.Contains(err.Error(), "changed after migration") {
		t.Fatalf("AppendEvent error = %v, want legacy-writer detection", err)
	}
}

func TestLegacyWriterCreatedAfterFreshV1StoreFailsClosed(t *testing.T) {
	repo, _ := newHookTestRepo(t, []string{"git", "status", "--short"})
	current := model.Event{
		SchemaVersion: model.SchemaVersion,
		ID:            "current-before-late-legacy",
		OccurredAt:    time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		Type:          model.SessionStarted,
		Source:        "current",
	}
	if err := AppendEvent(repo, current); err != nil {
		t.Fatal(err)
	}
	legacy := current
	legacy.ID = "late-legacy-writer"
	legacy.Source = "legacy"
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo.RuntimeDir, "events.jsonl"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEvents(repo); err == nil || !strings.Contains(err.Error(), "does not match the migrated first segment") {
		t.Fatalf("ReadEvents error = %v, want late legacy-writer detection", err)
	}
	current.ID = "current-after-late-legacy"
	if err := AppendEvent(repo, current); err == nil || !strings.Contains(err.Error(), "cannot prove") {
		t.Fatalf("AppendEvent error = %v, want late legacy-writer detection", err)
	}
}

func TestReadFailsClosedWhenMigrationStateIsMissing(t *testing.T) {
	repo, _ := newHookTestRepo(t, []string{"git", "status", "--short"})
	if err := repo.EnsureRuntime(); err != nil {
		t.Fatal(err)
	}
	legacy := model.Event{
		SchemaVersion: model.SchemaVersion,
		ID:            "legacy-with-missing-state",
		OccurredAt:    time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		Type:          model.SessionStarted,
		Source:        "legacy",
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo.RuntimeDir, "events.jsonl"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	current := legacy
	current.ID = "current-with-missing-state"
	if err := AppendEvent(repo, current); err != nil {
		t.Fatal(err)
	}
	store, err := ensureEventStoreDirs(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	if err := os.Remove(store.state); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEvents(repo); err == nil || !strings.Contains(err.Error(), "state is missing") {
		t.Fatalf("ReadEvents error = %v, want missing migration-state failure", err)
	}
}

func TestReadEventsFailsClosedOnMalformedOrOversizedLedger(t *testing.T) {
	repo, _ := newHookTestRepo(t, []string{"git", "status", "--short"})
	if err := repo.EnsureRuntime(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo.RuntimeDir, "events.jsonl")
	if err := os.WriteFile(path, []byte("{not-json}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEvents(repo); err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("malformed ledger error = %v", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxEventLedgerBytes + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEvents(repo); err == nil || !strings.Contains(err.Error(), "event ledger exceeds") {
		t.Fatalf("oversized ledger error = %v", err)
	}
	if err := os.Truncate(path, maxEventLedgerBytes-1); err != nil {
		t.Fatal(err)
	}
	event := model.Event{
		SchemaVersion: model.SchemaVersion,
		ID:            "ledger-boundary",
		OccurredAt:    time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC),
		Type:          model.SessionEnded,
		Source:        "fixture",
	}
	if err := AppendEvent(repo, event); err == nil || !strings.Contains(err.Error(), "missing newline") {
		t.Fatalf("ledger boundary append error = %v, want incomplete legacy ledger rejection", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != maxEventLedgerBytes-1 {
		t.Fatalf("rejected append changed ledger size to %d", info.Size())
	}
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	if err := AppendEvent(repo, event); err != nil {
		t.Fatalf("append after rejected oversized write failed: %v", err)
	}
}

func TestAppendRejectsLedgerWithoutFinalNewlineWithoutChangingIt(t *testing.T) {
	repo, _ := newHookTestRepo(t, []string{"git", "status", "--short"})
	if err := repo.EnsureRuntime(); err != nil {
		t.Fatal(err)
	}
	existing := model.Event{
		SchemaVersion: model.SchemaVersion,
		ID:            "incomplete-ledger-existing",
		OccurredAt:    time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC),
		Type:          model.SessionStarted,
		Source:        "fixture",
	}
	raw, err := json.Marshal(existing)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo.RuntimeDir, "events.jsonl")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEvents(repo); err == nil || !strings.Contains(err.Error(), "missing newline") {
		t.Fatalf("ReadEvents error = %v, want incomplete-record rejection", err)
	}
	next := existing
	next.ID = "must-not-be-concatenated"
	if err := AppendEvent(repo, next); err == nil || !strings.Contains(err.Error(), "missing newline") {
		t.Fatalf("AppendEvent error = %v, want incomplete-record rejection", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, raw) {
		t.Fatalf("rejected append mutated incomplete ledger: before=%q after=%q", raw, after)
	}
}

func TestEventLedgerLockDoesNotStealAnOldButActiveLock(t *testing.T) {
	repo, _ := newHookTestRepo(t, []string{"git", "status", "--short"})
	if err := repo.EnsureRuntime(); err != nil {
		t.Fatal(err)
	}
	release, err := acquireEventLedgerLock(repo)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(repo.RuntimeDir, "events.lock")
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		release()
		t.Fatal(err)
	}
	started := time.Now()
	secondRelease, err := acquireEventLedgerLock(repo)
	if secondRelease != nil {
		secondRelease()
	}
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		release()
		t.Fatalf("second lock acquisition = %v, want timeout while first owner is active", err)
	}
	if time.Since(started) < eventLockWait-(100*time.Millisecond) {
		release()
		t.Fatalf("active lock was stolen after %s", time.Since(started))
	}
	release()
	thirdRelease, err := acquireEventLedgerLock(repo)
	if err != nil {
		t.Fatalf("lock was not released by its owner: %v", err)
	}
	thirdRelease()
}

func TestReadEventsRemainAvailableWhileWriterLockIsHeld(t *testing.T) {
	repo, _ := newHookTestRepo(t, []string{"git", "status", "--short"})
	if err := repo.EnsureRuntime(); err != nil {
		t.Fatal(err)
	}
	release, err := acquireEventLedgerLock(repo)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := ReadEvents(repo)
		result <- err
	}()
	select {
	case err := <-result:
		if err != nil {
			release()
			t.Fatalf("lock-free ReadEvents failed while writer lock was held: %v", err)
		}
	case <-time.After(time.Second):
		release()
		t.Fatal("ReadEvents blocked behind the writer lock")
	}
	release()
}

func TestCodexSurfacesEventPersistenceFailure(t *testing.T) {
	newPoisonedRepo := func(t *testing.T) *repository.Repository {
		t.Helper()
		repo, _ := newHookTestRepo(t, []string{"git", "status", "--short"})
		if err := repo.EnsureRuntime(); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(repo.RuntimeDir, "events.jsonl"), 0o700); err != nil {
			t.Fatal(err)
		}
		return repo
	}

	t.Run("ordinary lifecycle hook returns an error", func(t *testing.T) {
		repo := newPoisonedRepo(t)
		input := strings.NewReader(`{"hook_event_name":"PostToolUse","session_id":"persist-error","tool_response":{"exit_code":0}}`)
		var out bytes.Buffer
		err := Codex(input, &out, repo.Root)
		if err == nil || !strings.Contains(err.Error(), "persist PostToolUse event") {
			t.Fatalf("Codex error = %v, want explicit persistence failure", err)
		}
	})

	t.Run("first Stop blocks", func(t *testing.T) {
		repo := newPoisonedRepo(t)
		output := runStopHook(t, repo.Root, false)
		if output.Decision != "block" || !strings.Contains(output.Reason, "timeline would be incomplete") {
			t.Fatalf("Stop output = %+v, want explicit persistence block", output)
		}
	})

	t.Run("second Stop warns but does not loop", func(t *testing.T) {
		repo := newPoisonedRepo(t)
		output := runStopHook(t, repo.Root, true)
		if output.Decision == "block" || !strings.Contains(output.SystemMessage, "timeline is incomplete") {
			t.Fatalf("loop-guard Stop output = %+v, want non-blocking persistence warning", output)
		}
	})
}

func TestReadEventsRejectsDuplicateIDsInExternallyModifiedLedger(t *testing.T) {
	repo, _ := newHookTestRepo(t, []string{"git", "status", "--short"})
	if err := repo.EnsureRuntime(); err != nil {
		t.Fatal(err)
	}
	event := model.Event{
		SchemaVersion: model.SchemaVersion,
		ID:            "externally-duplicated",
		OccurredAt:    time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC),
		Type:          model.SessionStarted,
		Source:        "fixture",
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	ledger := append(append(append([]byte{}, data...), '\n'), data...)
	ledger = append(ledger, '\n')
	if err := os.WriteFile(filepath.Join(repo.RuntimeDir, "events.jsonl"), ledger, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEvents(repo); err == nil || !strings.Contains(err.Error(), "duplicate event id") {
		t.Fatalf("duplicate ledger error = %v", err)
	}
}

func TestSubagentStopWritesRequiredJSONEnvelope(t *testing.T) {
	repo, _ := newHookTestRepo(t, []string{"git", "status", "--short"})
	input := strings.NewReader(`{"hook_event_name":"SubagentStop","session_id":"subagent-wire","cwd":"` + filepath.ToSlash(repo.Root) + `"}`)
	var out bytes.Buffer
	if err := Codex(input, &out, repo.Root); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "{}" {
		t.Fatalf("SubagentStop stdout = %q, want a valid empty JSON object", out.String())
	}
}

func TestCodexHookRejectsTrailingJSONValue(t *testing.T) {
	repo, _ := newHookTestRepo(t, []string{"git", "status", "--short"})
	input := strings.NewReader(`{"hook_event_name":"Stop","stop_hook_active":false} {"injected":true}`)
	var out bytes.Buffer
	if err := Codex(input, &out, repo.Root); err == nil {
		t.Fatal("Codex hook accepted a trailing JSON value")
	}
	if out.Len() != 0 {
		t.Fatalf("Codex hook wrote output for invalid input: %q", out.String())
	}
}

func TestStopValidationDeadlineBlocksBeforeOuterHookCanFailOpen(t *testing.T) {
	repo, _ := newHookTestRepo(t, []string{"git", "status", "--short"})
	input := strings.NewReader(`{"hook_event_name":"Stop","stop_hook_active":false,"session_id":"deadline","cwd":"` + filepath.ToSlash(repo.Root) + `"}`)
	var out bytes.Buffer
	if err := codexWithValidationBudget(input, &out, repo.Root, 0); err != nil {
		t.Fatal(err)
	}
	var result Output
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode deadline output %q: %v", out.String(), err)
	}
	if result.Decision != "block" || !strings.Contains(result.Reason, "safety budget") {
		t.Fatalf("deadline result = %+v, want an explicit blocking result", result)
	}
}

func TestInconclusiveReceiptWarnsWithoutPretendingPassOrFail(t *testing.T) {
	repo, cfg := newHookTestRepo(t, []string{"beforedone-missing-verifier-14c061"})
	result, err := checker.Run(repo, cfg, "unit")
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.Verdict != model.Inconclusive {
		t.Fatalf("fixture verdict = %s, want INCONCLUSIVE", result.Receipt.Verdict)
	}

	output := runStopHook(t, repo.Root, false)
	if output.Decision == "block" {
		t.Fatalf("INCONCLUSIVE Stop = %+v, want explicit warning without blocking", output)
	}
	if !strings.Contains(output.SystemMessage, "INCONCLUSIVE") {
		t.Fatalf("INCONCLUSIVE warning missing: %q", output.SystemMessage)
	}
}

func TestStopGateRejectsReceiptAliasForDifferentCheck(t *testing.T) {
	repo, cfg := newHookTestRepo(t, []string{"git", "status", "--short"})
	result, err := checker.Run(repo, cfg, "unit")
	if err != nil {
		t.Fatal(err)
	}
	result.Receipt.ID = "receipt-other"
	result.Receipt.CheckID = "other"
	if _, err := evidence.Save(repo, result.Receipt); err != nil {
		t.Fatal(err)
	}
	otherLatest := filepath.Join(repo.RuntimeDir, "receipts", "latest-other.json")
	data, err := os.ReadFile(otherLatest)
	if err != nil {
		t.Fatal(err)
	}
	unitLatest := filepath.Join(repo.RuntimeDir, "receipts", "latest-unit.json")
	if err := os.WriteFile(unitLatest, data, 0o600); err != nil {
		t.Fatal(err)
	}

	output := runStopHook(t, repo.Root, false)
	if output.Decision != "block" || !strings.Contains(output.Reason, "no evidence receipt") {
		t.Fatalf("mismatched latest alias bypassed Stop gate: %+v", output)
	}
}

func TestStopGateRejectsSignedPassWithNonzeroExit(t *testing.T) {
	repo, cfg := newHookTestRepo(t, []string{"git", "status", "--short"})
	if err := evidence.EnsureKey(repo); err != nil {
		t.Fatal(err)
	}
	fingerprint, count, err := evidence.Fingerprint(repo, cfg.Checks["unit"].RelevantFiles)
	if err != nil {
		t.Fatal(err)
	}
	logData := []byte("[stdout]\nPASS\n[stderr]\nfailed\n")
	logRel := filepath.ToSlash(filepath.Join("logs", "forged.log"))
	logPath := filepath.Join(repo.RuntimeDir, filepath.FromSlash(logRel))
	if err := os.WriteFile(logPath, logData, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(logData)
	commit, err := repo.Git("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	receipt := &model.Receipt{
		SchemaVersion:       1,
		ID:                  "receipt-forged",
		Producer:            "beforedone.check",
		CheckID:             "unit",
		Argv:                append([]string(nil), cfg.Checks["unit"].Argv...),
		WorkingDirectory:    ".",
		StartedAt:           now.Add(-time.Second),
		FinishedAt:          now,
		ExitCode:            0,
		Verdict:             model.Pass,
		GitCommit:           commit,
		RelevantFingerprint: fingerprint,
		RelevantFileCount:   count,
		LogPath:             logRel,
		LogSHA256:           "sha256:" + hex.EncodeToString(sum[:]),
		BeforeDoneVersion:   "attacker",
	}
	if _, err := evidence.Save(repo, receipt); err != nil {
		t.Fatal(err)
	}
	// Recreate the trivial forgery available to any coding agent with normal
	// repository read access: mutate the receipt, read the colocated key, and
	// recompute the HMAC without going through evidence.Sign.
	receipt.ExitCode = 1
	receipt.Signature = ""
	canonical, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	keyHex, err := os.ReadFile(filepath.Join(repo.RuntimeDir, "receipt.key"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := hex.DecodeString(string(keyHex))
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonical)
	receipt.Signature = "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil))
	forged, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo.RuntimeDir, "receipts", "latest-unit.json"), forged, 0o600); err != nil {
		t.Fatal(err)
	}

	output := runStopHook(t, repo.Root, false)
	if output.Decision != "block" {
		t.Fatalf("semantically forged PASS bypassed Stop gate: %+v", output)
	}
}

func runStopHook(t *testing.T, cwd string, stopActive bool) Output {
	t.Helper()
	input, err := json.Marshal(map[string]any{
		"hook_event_name":  "Stop",
		"stop_hook_active": stopActive,
		"session_id":       "test-session",
		"cwd":              cwd,
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Codex(bytes.NewReader(input), &out, cwd); err != nil {
		t.Fatal(err)
	}
	var result Output
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode hook output %q: %v", out.String(), err)
	}
	return result
}

func newHookTestRepo(t *testing.T, argv []string) (*repository.Repository, model.Config) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runHookGit(t, root, "init", "-q")
	runHookGit(t, root, "config", "user.email", "test@example.invalid")
	runHookGit(t, root, "config", "user.name", "BeforeDone Test")
	runHookGit(t, root, "add", ".")
	runHookGit(t, root, "commit", "-m", "fixture")
	repo, err := repository.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := model.Config{
		SchemaVersion: 1,
		Checks: map[string]model.CheckConfig{
			"unit": {Argv: argv, RelevantFiles: []string{"**/*.go"}, WorkingDirectory: ".", TimeoutSeconds: 120},
		},
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".beforedone.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return repo, cfg
}

func runHookGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}
