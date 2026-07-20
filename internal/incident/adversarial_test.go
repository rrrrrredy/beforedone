package incident

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rrrrrredy/beforedone/internal/model"
	"github.com/rrrrrredy/beforedone/internal/repository"
)

func TestIncidentRedactionRemovesJSONSecrets(t *testing.T) {
	output := redactText(`{"token":"incident-json-secret","note":"safe"}`, nil)
	if strings.Contains(output, "incident-json-secret") {
		t.Fatalf("JSON secret survived incident redaction: %q", output)
	}
}

func TestFirstObservableDivergencePrecisions(t *testing.T) {
	one := 1
	failure := receiptObservation{
		ID:         "receipt-unit-fail",
		CheckID:    "unit",
		Argv:       []string{"go", "test", "./..."},
		Verdict:    model.Fail,
		StartedAt:  time.Unix(20, 0),
		FinishedAt: time.Unix(30, 0),
	}

	t.Run("exact requires an explicit failing receipt association", func(t *testing.T) {
		events := []model.Event{
			{ID: "unrelated-nonzero", Type: model.ToolFinished, ExitCode: &one, OccurredAt: time.Unix(10, 0)},
			{ID: "later-verified-failure", Type: model.ToolFinished, ExitCode: &one, OccurredAt: time.Unix(32, 0), Attributes: map[string]string{"receipt_id": failure.ID, "check_id": "unit", "verdict": "FAIL", "argv": `["go","test","./..."]`}},
			{ID: "receipt-id-only-spoof", Type: model.ToolFinished, ExitCode: &one, OccurredAt: time.Unix(30, 0), Attributes: map[string]string{"receipt_id": failure.ID}},
			{ID: "earliest-verified-failure", Type: model.ToolFinished, ExitCode: &one, OccurredAt: time.Unix(31, 0), Attributes: map[string]string{"receipt_id": failure.ID, "check_id": "unit", "verdict": "FAIL", "argv": `["go","test","./..."]`}},
		}
		got := locateDivergence(events, []receiptObservation{failure})
		if got.Precision != model.ExactEvent || got.EventID != "earliest-verified-failure" || got.StartEvent != "" || got.EndEvent != "" {
			t.Fatalf("exact divergence = %+v", got)
		}
	})

	t.Run("check verdict and argv form a verifiable association", func(t *testing.T) {
		events := []model.Event{{
			ID:         "structured-failure",
			Type:       model.ToolFinished,
			ExitCode:   &one,
			OccurredAt: time.Unix(31, 0),
			Attributes: map[string]string{"check_id": "unit", "verdict": "FAIL", "argv": `["go","test","./..."]`},
		}}
		got := locateDivergence(events, []receiptObservation{failure})
		if got.Precision != model.ExactEvent || got.EventID != "structured-failure" {
			t.Fatalf("structured exact divergence = %+v", got)
		}
	})

	t.Run("window requires observed events enclosing the failing receipt", func(t *testing.T) {
		events := []model.Event{
			{ID: "after-check", Type: model.AgentStopping, OccurredAt: time.Unix(40, 0)},
			{ID: "before-check", Type: model.ToolStarted, OccurredAt: time.Unix(10, 0)},
		}
		got := locateDivergence(events, []receiptObservation{failure})
		if got.Precision != model.TimeWindow || got.StartEvent != "before-check" || got.EndEvent != "after-check" || got.EventID != "" {
			t.Fatalf("time-window divergence = %+v", got)
		}
	})

	t.Run("receipt metadata outside the delivery window is not exact", func(t *testing.T) {
		event := model.Event{
			ID:         "stale-metadata",
			Type:       model.ToolFinished,
			ExitCode:   &one,
			OccurredAt: failure.FinishedAt.Add(6 * time.Minute),
			Attributes: map[string]string{"receipt_id": failure.ID, "check_id": "unit", "verdict": "FAIL", "argv": `["go","test","./..."]`},
		}
		if eventMatchesReceipt(event, failure) {
			t.Fatal("event carrying old receipt metadata was accepted as an exact association")
		}
	})

	t.Run("unlocated rejects arbitrary failures and unproved boundaries", func(t *testing.T) {
		events := []model.Event{
			{ID: "unrelated-nonzero", Type: model.ToolFinished, ExitCode: &one, OccurredAt: time.Unix(25, 0)},
			{ID: "correction", Type: model.PromptSubmitted, Summary: "still broken", OccurredAt: time.Unix(35, 0)},
		}
		got := locateDivergence(events, []receiptObservation{failure})
		if got.Precision != model.Unlocated || got.EventID != "" || got.StartEvent != "" || got.EndEvent != "" {
			t.Fatalf("unlocated divergence = %+v", got)
		}
	})
}

func TestIncidentVerdictPrecedenceIsOrderIndependent(t *testing.T) {
	permutations := [][]model.Verdict{
		{model.Pass, model.Inconclusive, model.Fail},
		{model.Fail, model.Pass, model.Inconclusive},
		{model.Inconclusive, model.Fail, model.Pass},
	}
	for _, sequence := range permutations {
		got := model.Pass
		for _, verdict := range sequence {
			got = strongerVerdict(got, verdict)
		}
		if got != model.Fail {
			t.Fatalf("strongest verdict for %v = %s, want FAIL", sequence, got)
		}
	}
	if got := strongerVerdict(model.Pass, model.Inconclusive); got != model.Inconclusive {
		t.Fatalf("PASS + INCONCLUSIVE = %s, want INCONCLUSIVE", got)
	}
}

func TestLatestSessionRetainsParentFailureWhenSubagentFinishesLater(t *testing.T) {
	one := 1
	events := []model.Event{
		{ID: "old-root", Type: model.SessionStarted, SessionID: "old", OccurredAt: time.Unix(1, 0)},
		{ID: "old-tool", Type: model.ToolFinished, SessionID: "old", OccurredAt: time.Unix(2, 0)},
		{ID: "parent-root", Type: model.SessionStarted, SessionID: "parent", OccurredAt: time.Unix(10, 0)},
		{ID: "parent-failure", Type: model.ToolFinished, SessionID: "parent", ExitCode: &one, OccurredAt: time.Unix(11, 0)},
		{ID: "child-start", Type: model.SessionStarted, SessionID: "child", OccurredAt: time.Unix(12, 0), Attributes: map[string]string{"agent_id": "child-1", "agent_type": "reviewer"}},
		{ID: "child-finish", Type: model.ToolFinished, SessionID: "child", OccurredAt: time.Unix(13, 0), Attributes: map[string]string{"agent_id": "child-1"}},
	}
	got := latestSession(events)
	if len(got) != 4 || got[0].ID != "parent-root" || got[1].ID != "parent-failure" || got[3].ID != "child-finish" {
		t.Fatalf("latest root session segment = %+v, want parent failure and later child events", got)
	}
}

func TestLatestSessionWithoutRootBoundaryDoesNotDropObservableEvents(t *testing.T) {
	events := []model.Event{
		{ID: "parent-event", Type: model.ToolFinished, SessionID: "parent", OccurredAt: time.Unix(1, 0)},
		{ID: "child-event", Type: model.ToolFinished, SessionID: "child", OccurredAt: time.Unix(2, 0), Attributes: map[string]string{"agent_id": "child-1"}},
	}
	got := latestSession(events)
	if len(got) != len(events) || got[0].ID != "parent-event" {
		t.Fatalf("fallback latestSession = %+v, want all observable events", got)
	}
}

func TestHTMLReportEscapesUntrustedContentAndHasStrictCSP(t *testing.T) {
	payload := `</pre><script>alert("xss")</script><img src=x onerror=alert(2)>`
	report := model.Incident{
		SchemaVersion: 1,
		ID:            payload,
		CreatedAt:     time.Now().UTC(),
		Verdict:       model.Inconclusive,
		FirstObservableDivergence: model.Divergence{
			Precision: model.Unlocated,
			Reason:    payload,
		},
		Timeline:   []model.Event{{ID: "event", Type: model.ToolFinished, Summary: payload}},
		Evidence:   []model.EvidenceItem{{Claim: payload, Evidence: payload, Status: payload}},
		Correction: payload,
		Transcript: &model.TranscriptSupplement{SHA256: "sha256:" + strings.Repeat("a", 64), Excerpt: payload, Statement: payload},
		NextSteps:  []string{payload},
	}
	path := filepath.Join(t.TempDir(), "report.html")
	if err := writeHTML(path, report); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	lower := strings.ToLower(html)
	for _, forbidden := range []string{"<script", "<img src=x", "javascript:", "<iframe"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("untrusted HTML was emitted as active markup (%q)", forbidden)
		}
	}
	for _, directive := range []string{"default-src 'none'", "base-uri 'none'", "form-action 'none'"} {
		if !strings.Contains(html, directive) {
			t.Fatalf("CSP is missing %q", directive)
		}
	}
	if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") {
		t.Fatal("self-contained incident report references a network resource")
	}
}

func TestTranscriptSupplementIsBoundedRedactedAndNarrativeOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	content := strings.Repeat("observable tool event\n", 1000) + "token=super-secret-value\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	supplement, err := readTranscriptSupplement(path, []string{`(?i)token\s*=\s*[^\s]+`})
	if err != nil {
		t.Fatal(err)
	}
	if !supplement.Truncated || len(supplement.Excerpt) > (16<<10)+len("…") {
		t.Fatalf("transcript excerpt was not bounded: truncated=%t bytes=%d", supplement.Truncated, len(supplement.Excerpt))
	}
	if strings.Contains(supplement.Excerpt, "super-secret-value") {
		t.Fatal("transcript secret survived redaction")
	}
	if !strings.HasPrefix(supplement.SHA256, "sha256:") || !strings.Contains(supplement.Statement, "not used") {
		t.Fatalf("transcript provenance/boundary missing: %+v", supplement)
	}

	oversized := filepath.Join(t.TempDir(), "oversized.log")
	if err := os.WriteFile(oversized, make([]byte, (4<<20)+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readTranscriptSupplement(oversized, nil); err == nil || !strings.Contains(err.Error(), "exceeds 4 MiB") {
		t.Fatalf("oversized transcript error = %v", err)
	}
}

func TestCreateRedactsUserCorrectionBeforePersisting(t *testing.T) {
	repo := newIncidentTestRepo(t)
	cfg := model.Config{
		SchemaVersion: 1,
		Checks: map[string]model.CheckConfig{
			"unit": {Argv: []string{"git", "status", "--short"}, RelevantFiles: []string{"**/*"}, WorkingDirectory: "."},
		},
	}
	correction := "please retry {\"password\":\"prefix,INCIDENT-COMMA-TAIL-LEAK\"} {'token':'prefix;INCIDENT-SEMI-TAIL-LEAK'}\nAuthorization: Digest username=\"alice\", response=\"INCIDENT-DIGEST-LEAK\"\n%22token%22%3D%22INCIDENT-PERCENT-LEAK%22 {\"pass\\u0077ord\":\"INCIDENT-UNICODE-LEAK\"}"
	artifacts, err := Create(repo, cfg, correction)
	if err != nil {
		t.Fatal(err)
	}
	secrets := []string{"INCIDENT-COMMA-TAIL-LEAK", "INCIDENT-SEMI-TAIL-LEAK", "INCIDENT-DIGEST-LEAK", "INCIDENT-PERCENT-LEAK", "INCIDENT-UNICODE-LEAK"}
	for _, secret := range secrets {
		if strings.Contains(artifacts.Incident.Correction, secret) {
			t.Fatalf("secret persisted in incident correction: %q", artifacts.Incident.Correction)
		}
	}
	for _, path := range []string{artifacts.JSONPath, artifacts.HTMLPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range secrets {
			if strings.Contains(string(data), secret) {
				t.Fatalf("secret %q persisted in %s", secret, path)
			}
		}
	}
}

func newIncidentTestRepo(t *testing.T) *repository.Repository {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runIncidentGit(t, root, "init", "-q")
	runIncidentGit(t, root, "config", "user.email", "test@example.invalid")
	runIncidentGit(t, root, "config", "user.name", "BeforeDone Test")
	runIncidentGit(t, root, "add", ".")
	runIncidentGit(t, root, "commit", "-m", "fixture")
	repo, err := repository.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func runIncidentGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}
