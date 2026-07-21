package incident

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rrrrrredy/beforedone/internal/evidence"
	"github.com/rrrrrredy/beforedone/internal/hooks"
	"github.com/rrrrrredy/beforedone/internal/model"
	"github.com/rrrrrredy/beforedone/internal/redact"
	"github.com/rrrrrredy/beforedone/internal/repository"
)

type Artifacts struct {
	Directory  string         `json:"directory"`
	JSONPath   string         `json:"incident_json"`
	HTMLPath   string         `json:"report_html"`
	ReplayPath string         `json:"replay_case"`
	Incident   model.Incident `json:"incident"`
}

func Create(repo *repository.Repository, cfg model.Config, correction string) (*Artifacts, error) {
	return CreateWithTranscript(repo, cfg, correction, "")
}

func CreateWithTranscript(repo *repository.Repository, cfg model.Config, correction, transcriptPath string) (*Artifacts, error) {
	if err := repo.EnsureRuntime(); err != nil {
		return nil, err
	}
	events, err := hooks.ReadEvents(repo)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].OccurredAt.Before(events[j].OccurredAt) })
	events = latestSession(events)
	verdict, matrix, missing, receipts := assessReceipts(repo, cfg)
	fod := locateDivergence(events, receipts)
	head, _ := repo.Git("rev-parse", "HEAD")
	base, _ := repo.Git("rev-parse", "HEAD^")
	diff := diffSummary(repo, base, head)
	transcript, err := readTranscriptSupplement(transcriptPath, cfg.Capture.RedactPatterns)
	if err != nil {
		return nil, err
	}
	id := evidence.NewID("incident")
	report := model.Incident{
		SchemaVersion:             model.SchemaVersion,
		ID:                        id,
		CreatedAt:                 time.Now().UTC(),
		Repository:                filepath.Base(repo.Root),
		BaseCommit:                base,
		HeadCommit:                head,
		Verdict:                   verdict,
		FirstObservableDivergence: fod,
		Timeline:                  events,
		Evidence:                  matrix,
		MissingEvidence:           missing,
		Correction:                redactCorrection(correction, cfg.Capture.RedactPatterns),
		Transcript:                transcript,
		DiffSummary:               diff,
		NextSteps:                 nextSteps(verdict, missing),
	}
	replay := model.ReplayCase{
		SchemaVersion: model.SchemaVersion,
		ID:            evidence.NewID("replay"),
		CreatedAt:     time.Now().UTC(),
		IncidentID:    id,
		BaseCommit:    base,
		HeadCommit:    head,
		Verdict:       verdict,
		Divergence:    fod,
	}
	for _, event := range events {
		replay.EventIDs = append(replay.EventIDs, event.ID)
	}

	dir := filepath.Join(repo.RuntimeDir, "incidents", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	jsonPath := filepath.Join(dir, "incident.json")
	htmlPath := filepath.Join(dir, "report.html")
	replayPath := filepath.Join(dir, "replay-case.json")
	if err := writeJSON(jsonPath, report); err != nil {
		return nil, err
	}
	if err := writeJSON(replayPath, replay); err != nil {
		return nil, err
	}
	if err := writeHTML(htmlPath, report); err != nil {
		return nil, err
	}
	latestReplay := filepath.Join(repo.RuntimeDir, "latest-replay-case.json")
	if err := writeJSON(latestReplay, replay); err != nil {
		return nil, err
	}
	latestIncident := filepath.Join(repo.RuntimeDir, "latest-incident.json")
	if err := writeJSON(latestIncident, report); err != nil {
		return nil, err
	}
	prune(repo, cfg.Reports.Retain)
	return &Artifacts{Directory: dir, JSONPath: jsonPath, HTMLPath: htmlPath, ReplayPath: replayPath, Incident: report}, nil
}

func readTranscriptSupplement(path string, configured []string) (*model.TranscriptSupplement, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open transcript supplement: %w", err)
	}
	defer f.Close()
	const maxInput = 4 << 20
	raw, err := io.ReadAll(io.LimitReader(f, maxInput+1))
	if err != nil {
		return nil, fmt.Errorf("read transcript supplement: %w", err)
	}
	if len(raw) > maxInput {
		return nil, errors.New("transcript supplement exceeds 4 MiB")
	}
	sum := sha256.Sum256(raw)
	redacted := redactText(string(raw), configured)
	const excerptLimit = 16 << 10
	truncated := len(redacted) > excerptLimit
	if truncated {
		redacted = redacted[:excerptLimit] + "…"
	}
	return &model.TranscriptSupplement{
		SHA256:    "sha256:" + hex.EncodeToString(sum[:]),
		Excerpt:   redacted,
		Truncated: truncated,
		Statement: "Narrative supplement only. It was not used to locate the First Observable Divergence or prove a claim.",
	}, nil
}

func redactCorrection(value string, configured []string) string {
	return sanitize(redactText(value, configured), 4000)
}

func redactText(value string, configured []string) string {
	return redact.BestEffort(value, configured)
}

func latestSession(events []model.Event) []model.Event {
	// A subagent has its own session_id and can emit the final event in the
	// ledger. Selecting that ID would discard the parent failure that the
	// incident is meant to explain. Instead, start at the latest root
	// SessionStarted event and retain the complete parent/subagent segment.
	rootStart := -1
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == model.SessionStarted && !isSubagentEvent(events[i]) {
			rootStart = i
			break
		}
	}
	if rootStart < 0 {
		// Without a root boundary, retaining all observable events is safer than
		// silently dropping a parent session based on a late child event.
		return events
	}
	return events[rootStart:]
}

func isSubagentEvent(event model.Event) bool {
	if event.Attributes == nil {
		return false
	}
	return strings.TrimSpace(event.Attributes["agent_id"]) != "" || strings.TrimSpace(event.Attributes["agent_type"]) != ""
}

type receiptObservation struct {
	ID         string
	CheckID    string
	Argv       []string
	Verdict    model.Verdict
	StartedAt  time.Time
	FinishedAt time.Time
}

func assessReceipts(repo *repository.Repository, cfg model.Config) (model.Verdict, []model.EvidenceItem, []string, []receiptObservation) {
	verdict := model.Pass
	var matrix []model.EvidenceItem
	var missing []string
	var observations []receiptObservation
	ids := make([]string, 0, len(cfg.Checks))
	for id, check := range cfg.Checks {
		if check.IsRequired() {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		receipt, err := evidence.LoadLatest(repo, id)
		if err != nil {
			verdict = strongerVerdict(verdict, model.Inconclusive)
			missing = append(missing, id+": missing receipt")
			matrix = append(matrix, model.EvidenceItem{Claim: id + " passed", Evidence: "No receipt", Status: "MISSING"})
			continue
		}
		if err := evidence.VerifySignature(repo, receipt); err != nil {
			verdict = strongerVerdict(verdict, model.Inconclusive)
			missing = append(missing, id+": invalid receipt")
			matrix = append(matrix, model.EvidenceItem{Claim: id + " passed", Evidence: err.Error(), Status: "INVALID"})
			continue
		}
		observations = append(observations, receiptObservation{
			ID:         receipt.ID,
			CheckID:    receipt.CheckID,
			Argv:       append([]string(nil), receipt.Argv...),
			Verdict:    receipt.Verdict,
			StartedAt:  receipt.StartedAt,
			FinishedAt: receipt.FinishedAt,
		})
		if receipt.Verdict == model.Fail {
			verdict = strongerVerdict(verdict, model.Fail)
			matrix = append(matrix, model.EvidenceItem{Claim: id + " passed", Evidence: receipt.ID, Status: "CONTRADICTED"})
			continue
		}
		if receipt.Verdict == model.Inconclusive {
			verdict = strongerVerdict(verdict, model.Inconclusive)
			missing = append(missing, id+": inconclusive check")
			matrix = append(matrix, model.EvidenceItem{Claim: id + " passed", Evidence: receipt.ID, Status: "INCONCLUSIVE"})
			continue
		}
		fresh, reason := evidence.ValidateFresh(repo, cfg, receipt)
		if !fresh {
			verdict = strongerVerdict(verdict, model.Inconclusive)
			missing = append(missing, id+": "+reason)
			matrix = append(matrix, model.EvidenceItem{Claim: id + " passed", Evidence: receipt.ID, Status: "STALE"})
		} else {
			matrix = append(matrix, model.EvidenceItem{Claim: id + " passed", Evidence: receipt.ID, Status: "SUPPORTED"})
		}
	}
	if len(ids) == 0 {
		return model.Inconclusive, matrix, []string{"no required checks configured"}, observations
	}
	return verdict, matrix, missing, observations
}

func strongerVerdict(current, candidate model.Verdict) model.Verdict {
	rank := func(verdict model.Verdict) int {
		switch verdict {
		case model.Fail:
			return 2
		case model.Inconclusive:
			return 1
		default:
			return 0
		}
	}
	if rank(candidate) > rank(current) {
		return candidate
	}
	return current
}

func locateDivergence(events []model.Event, receipts []receiptObservation) model.Divergence {
	orderedEvents := append([]model.Event(nil), events...)
	sort.SliceStable(orderedEvents, func(i, j int) bool {
		left, right := orderedEvents[i].OccurredAt, orderedEvents[j].OccurredAt
		if left.IsZero() != right.IsZero() {
			return !left.IsZero()
		}
		return left.Before(right)
	})
	// A generic non-zero tool exit is not enough: it may be an expected probe or
	// an unrelated command. An exact event must carry an explicit, verifiable
	// association to a signed failing BeforeDone receipt.
	for _, event := range orderedEvents {
		for _, receipt := range receipts {
			if receipt.Verdict == model.Fail && eventMatchesReceipt(event, receipt) {
				return model.Divergence{Precision: model.ExactEvent, EventID: event.ID, Reason: "This event is explicitly associated with a verified failing BeforeDone receipt."}
			}
		}
	}

	// If the failing receipt has no explicitly linked event, its signed start
	// and finish timestamps can still provide an honest window when two observed
	// events fully enclose that interval. A user correction alone is not such a
	// boundary and therefore cannot manufacture a window.
	failures := make([]receiptObservation, 0, len(receipts))
	for _, receipt := range receipts {
		if receipt.Verdict == model.Fail {
			failures = append(failures, receipt)
		}
	}
	sort.SliceStable(failures, func(i, j int) bool { return failures[i].FinishedAt.Before(failures[j].FinishedAt) })
	if len(failures) > 0 {
		receipt := failures[0]
		start, end := enclosingEvents(orderedEvents, receipt.StartedAt, receipt.FinishedAt)
		if start != "" && end != "" {
			return model.Divergence{Precision: model.TimeWindow, StartEvent: start, EndEvent: end, Reason: "A verified failing BeforeDone check ran entirely between these two observed events, but no event is explicitly linked to its receipt."}
		}
	}
	return model.Divergence{Precision: model.Unlocated, Reason: "No event provides enough evidence to locate a divergence."}
}

func eventMatchesReceipt(event model.Event, receipt receiptObservation) bool {
	if event.Type != model.ToolFinished || event.ID == "" || event.Attributes == nil || event.ExitCode == nil || *event.ExitCode == 0 {
		return false
	}
	if receipt.Verdict != model.Fail || receipt.FinishedAt.IsZero() || event.OccurredAt.IsZero() {
		return false
	}
	// Codex emits PostToolUse after the checked command exits. Permit a bounded
	// delivery delay, but reject adapters that attach old receipt metadata to an
	// unrelated event.
	if event.OccurredAt.Before(receipt.FinishedAt) || event.OccurredAt.After(receipt.FinishedAt.Add(5*time.Minute)) {
		return false
	}
	if claimedID := event.Attributes["receipt_id"]; claimedID != "" && claimedID != receipt.ID {
		return false
	}
	if event.Attributes["check_id"] != receipt.CheckID || event.Attributes["verdict"] != string(receipt.Verdict) {
		return false
	}
	var argv []string
	dec := json.NewDecoder(strings.NewReader(event.Attributes["argv"]))
	if err := dec.Decode(&argv); err != nil {
		return false
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return false
	}
	if len(argv) != len(receipt.Argv) {
		return false
	}
	for i := range argv {
		if argv[i] != receipt.Argv[i] {
			return false
		}
	}
	return true
}

func enclosingEvents(events []model.Event, startedAt, finishedAt time.Time) (string, string) {
	if startedAt.IsZero() || finishedAt.IsZero() || finishedAt.Before(startedAt) {
		return "", ""
	}
	startIndex := -1
	endIndex := -1
	for i, event := range events {
		if event.ID == "" || event.OccurredAt.IsZero() {
			continue
		}
		if !event.OccurredAt.After(startedAt) {
			startIndex = i
		}
		if endIndex < 0 && !event.OccurredAt.Before(finishedAt) {
			endIndex = i
		}
	}
	if startIndex < 0 || endIndex < 0 || startIndex >= endIndex {
		return "", ""
	}
	return events[startIndex].ID, events[endIndex].ID
}

func diffSummary(repo *repository.Repository, base, head string) string {
	var result string
	if base != "" && head != "" {
		result, _ = repo.Git("diff", "--stat", "--no-ext-diff", base, head)
	}
	working, _ := repo.Git("diff", "--stat", "--no-ext-diff", "HEAD")
	if working != "" {
		if result != "" {
			result += "\n"
		}
		result += "Uncommitted changes:\n" + working
	}
	return sanitize(result, 12000)
}

func nextSteps(verdict model.Verdict, missing []string) []string {
	var result []string
	if verdict == model.Fail {
		result = append(result, "Fix the first failing check, then run it again through `beforedone check <id>`.")
	}
	if len(missing) > 0 {
		result = append(result, "Collect fresh evidence for every missing, stale, or inconclusive required check.")
	}
	result = append(result, "Run `beforedone replay analyze` to reproduce this evidence-only classification.")
	return result
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func writeHTML(path string, report model.Incident) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return reportTemplate.Execute(f, report)
}

func sanitize(value string, limit int) string {
	patterns := []*strings.Replacer{
		strings.NewReplacer("\x00", ""),
	}
	for _, r := range patterns {
		value = r.Replace(value)
	}
	value = strings.TrimSpace(value)
	if len(value) > limit {
		value = value[:limit] + "…"
	}
	return value
}

func prune(repo *repository.Repository, retain int) {
	if retain <= 0 {
		return
	}
	dir := filepath.Join(repo.RuntimeDir, "incidents")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= retain {
		return
	}
	type candidate struct {
		name string
		time time.Time
	}
	var dirs []candidate
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "incident-") {
			continue
		}
		info, err := entry.Info()
		if err == nil {
			dirs = append(dirs, candidate{entry.Name(), info.ModTime()})
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].time.Before(dirs[j].time) })
	for len(dirs) > retain {
		path := filepath.Join(dir, dirs[0].name)
		if repository.IsWithin(dir, path) {
			_ = os.RemoveAll(path)
		}
		dirs = dirs[1:]
	}
}

var reportTemplate = template.Must(template.New("incident").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'">
<title>BeforeDone Incident {{.ID}}</title>
<style>
:root{color-scheme:dark;background:#0b0c10;color:#f4f1e8;font:16px/1.55 ui-sans-serif,system-ui,sans-serif}body{max-width:1100px;margin:auto;padding:48px 24px}h1,h2{letter-spacing:-.03em}.hero{border:1px solid #34383f;border-radius:22px;padding:30px;background:#12141a}.verdict{display:inline-block;padding:5px 11px;border-radius:99px;background:#e6ff65;color:#10120d;font-weight:800}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:16px}.card{border:1px solid #34383f;border-radius:16px;padding:18px;background:#151820}code,pre{font-family:ui-monospace,monospace}pre{white-space:pre-wrap;overflow-wrap:anywhere;background:#090a0d;padding:16px;border-radius:12px}.muted{color:#aeb4bd}table{width:100%;border-collapse:collapse}th,td{text-align:left;border-bottom:1px solid #34383f;padding:10px;vertical-align:top}
</style></head><body><main>
<section class="hero"><p class="verdict">{{.Verdict}}</p><h1>BeforeDone Incident Report</h1><p class="muted">{{.ID}} · {{.CreatedAt}}</p><p>This report reconstructs observable evidence. It does not claim access to hidden chain-of-thought.</p></section>
<h2>First Observable Divergence</h2><section class="card"><strong>{{.FirstObservableDivergence.Precision}}</strong><p>{{.FirstObservableDivergence.Reason}}</p>{{if .FirstObservableDivergence.EventID}}<code>{{.FirstObservableDivergence.EventID}}</code>{{end}}{{if .FirstObservableDivergence.StartEvent}}<code>{{.FirstObservableDivergence.StartEvent}} → {{.FirstObservableDivergence.EndEvent}}</code>{{end}}</section>
<h2>Claim / Evidence Matrix</h2><table><thead><tr><th>Claim</th><th>Evidence</th><th>Status</th></tr></thead><tbody>{{range .Evidence}}<tr><td>{{.Claim}}</td><td><code>{{.Evidence}}</code></td><td>{{.Status}}</td></tr>{{end}}</tbody></table>
<h2>Timeline</h2><div class="grid">{{range .Timeline}}<article class="card"><strong>{{.Type}}</strong><p class="muted">{{.OccurredAt}} · {{.ID}}</p>{{if .ToolName}}<p>Tool: <code>{{.ToolName}}</code></p>{{end}}{{if .Summary}}<pre>{{.Summary}}</pre>{{end}}</article>{{else}}<p class="muted">No normalized events were captured.</p>{{end}}</div>
{{if .MissingEvidence}}<h2>Missing or invalid evidence</h2><ul>{{range .MissingEvidence}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{if .Correction}}<h2>User correction</h2><pre>{{.Correction}}</pre>{{end}}
{{if .Transcript}}<h2>Optional transcript supplement</h2><section class="card"><p>{{.Transcript.Statement}}</p><p class="muted"><code>{{.Transcript.SHA256}}</code>{{if .Transcript.Truncated}} · excerpt truncated{{end}}</p><pre>{{.Transcript.Excerpt}}</pre></section>{{end}}
{{if .DiffSummary}}<h2>Git diff summary</h2><pre>{{.DiffSummary}}</pre>{{end}}
<h2>Next steps</h2><ol>{{range .NextSteps}}<li>{{.}}</li>{{end}}</ol>
</main></body></html>`))

var _ = errors.New
var _ = fmt.Sprintf
