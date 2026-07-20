package model

import "time"

const SchemaVersion = 1

type Verdict string

const (
	Pass         Verdict = "PASS"
	Fail         Verdict = "FAIL"
	Inconclusive Verdict = "INCONCLUSIVE"
)

func (v Verdict) Valid() bool {
	return v == Pass || v == Fail || v == Inconclusive
}

type Config struct {
	SchemaVersion int                    `yaml:"schema_version" json:"schema_version"`
	Checks        map[string]CheckConfig `yaml:"checks" json:"checks"`
	Capture       CaptureConfig          `yaml:"capture,omitempty" json:"capture"`
	Reports       ReportConfig           `yaml:"reports,omitempty" json:"reports"`
}

type CheckConfig struct {
	Argv             []string `yaml:"argv" json:"argv"`
	RelevantFiles    []string `yaml:"relevant_files" json:"relevant_files"`
	WorkingDirectory string   `yaml:"working_directory,omitempty" json:"working_directory,omitempty"`
	TimeoutSeconds   int      `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	Required         *bool    `yaml:"required,omitempty" json:"required,omitempty"`
}

func (c CheckConfig) IsRequired() bool { return c.Required == nil || *c.Required }

type CaptureConfig struct {
	MaxOutputBytes int64    `yaml:"max_output_bytes,omitempty" json:"max_output_bytes"`
	RedactPatterns []string `yaml:"redact_patterns,omitempty" json:"redact_patterns"`
}

type ReportConfig struct {
	Retain int `yaml:"retain,omitempty" json:"retain"`
}

type Receipt struct {
	SchemaVersion       int       `json:"schema_version"`
	ID                  string    `json:"id"`
	Producer            string    `json:"producer"`
	CheckID             string    `json:"check_id"`
	Argv                []string  `json:"argv"`
	WorkingDirectory    string    `json:"working_directory"`
	StartedAt           time.Time `json:"started_at"`
	FinishedAt          time.Time `json:"finished_at"`
	ExitCode            int       `json:"exit_code"`
	Verdict             Verdict   `json:"verdict"`
	GitCommit           string    `json:"git_commit"`
	RelevantFingerprint string    `json:"relevant_fingerprint"`
	RelevantFileCount   int       `json:"relevant_file_count"`
	StdoutSummary       string    `json:"stdout_summary,omitempty"`
	StderrSummary       string    `json:"stderr_summary,omitempty"`
	LogPath             string    `json:"log_path"`
	LogSHA256           string    `json:"log_sha256"`
	BeforeDoneVersion   string    `json:"beforedone_version"`
	Error               string    `json:"error,omitempty"`
	Signature           string    `json:"signature"`
	Fresh               *bool     `json:"fresh,omitempty"`
	StaleReason         string    `json:"stale_reason,omitempty"`
}

type EventType string

const (
	SessionStarted  EventType = "SessionStarted"
	PromptSubmitted EventType = "PromptSubmitted"
	ToolStarted     EventType = "ToolStarted"
	ToolFinished    EventType = "ToolFinished"
	AgentStopping   EventType = "AgentStopping"
	SessionEnded    EventType = "SessionEnded"
)

func (t EventType) Valid() bool {
	switch t {
	case SessionStarted, PromptSubmitted, ToolStarted, ToolFinished, AgentStopping, SessionEnded:
		return true
	default:
		return false
	}
}

type Event struct {
	SchemaVersion int               `json:"schema_version"`
	ID            string            `json:"id"`
	OccurredAt    time.Time         `json:"occurred_at"`
	Type          EventType         `json:"type"`
	Source        string            `json:"source"`
	SessionID     string            `json:"session_id,omitempty"`
	TurnID        string            `json:"turn_id,omitempty"`
	CWD           string            `json:"cwd,omitempty"`
	ToolName      string            `json:"tool_name,omitempty"`
	ExitCode      *int              `json:"exit_code,omitempty"`
	Summary       string            `json:"summary,omitempty"`
	Attributes    map[string]string `json:"attributes,omitempty"`
}

type FODPrecision string

const (
	ExactEvent FODPrecision = "exact_event"
	TimeWindow FODPrecision = "time_window"
	Unlocated  FODPrecision = "unlocated"
)

type Divergence struct {
	Precision  FODPrecision `json:"precision"`
	EventID    string       `json:"event_id,omitempty"`
	StartEvent string       `json:"start_event,omitempty"`
	EndEvent   string       `json:"end_event,omitempty"`
	Reason     string       `json:"reason"`
}

type EvidenceItem struct {
	Claim    string `json:"claim"`
	Evidence string `json:"evidence"`
	Status   string `json:"status"`
}

type TranscriptSupplement struct {
	SHA256    string `json:"sha256"`
	Excerpt   string `json:"excerpt"`
	Truncated bool   `json:"truncated"`
	Statement string `json:"statement"`
}

type Incident struct {
	SchemaVersion             int                   `json:"schema_version"`
	ID                        string                `json:"id"`
	CreatedAt                 time.Time             `json:"created_at"`
	Repository                string                `json:"repository"`
	BaseCommit                string                `json:"base_commit,omitempty"`
	HeadCommit                string                `json:"head_commit,omitempty"`
	Verdict                   Verdict               `json:"verdict"`
	FirstObservableDivergence Divergence            `json:"first_observable_divergence"`
	Timeline                  []Event               `json:"timeline"`
	Evidence                  []EvidenceItem        `json:"claim_evidence_matrix"`
	MissingEvidence           []string              `json:"missing_evidence,omitempty"`
	Correction                string                `json:"user_correction,omitempty"`
	Transcript                *TranscriptSupplement `json:"transcript_supplement,omitempty"`
	DiffSummary               string                `json:"diff_summary,omitempty"`
	NextSteps                 []string              `json:"next_steps"`
}

type ReplayCase struct {
	SchemaVersion int        `json:"schema_version"`
	ID            string     `json:"id"`
	CreatedAt     time.Time  `json:"created_at"`
	IncidentID    string     `json:"incident_id"`
	BaseCommit    string     `json:"base_commit,omitempty"`
	HeadCommit    string     `json:"head_commit,omitempty"`
	Verdict       Verdict    `json:"verdict"`
	Divergence    Divergence `json:"first_observable_divergence"`
	EventIDs      []string   `json:"event_ids,omitempty"`
	// Imported commands are retained only as inert evidence. replay verify ignores them.
	ObservedCommands [][]string `json:"observed_commands,omitempty"`
}

type AdapterManifest struct {
	SchemaVersion int               `json:"schema_version"`
	Name          string            `json:"name"`
	Version       string            `json:"version"`
	Capabilities  map[string]bool   `json:"capabilities"`
	EventMap      map[string]string `json:"event_map,omitempty"`
}
