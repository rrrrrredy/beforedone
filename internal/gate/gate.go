package gate

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rrrrrredy/beforedone/internal/evidence"
	"github.com/rrrrrredy/beforedone/internal/model"
	"github.com/rrrrrredy/beforedone/internal/repository"
)

const DefaultValidationBudget = 45 * time.Second

type Decision string

const (
	Allow Decision = "allow"
	Block Decision = "block"
)

type CheckResult struct {
	CheckID        string        `json:"check_id"`
	Verdict        model.Verdict `json:"verdict"`
	ReceiptVerdict model.Verdict `json:"receipt_verdict,omitempty"`
	Fresh          *bool         `json:"fresh,omitempty"`
	Reason         string        `json:"reason,omitempty"`
}

type Result struct {
	SchemaVersion int           `json:"schema_version"`
	Decision      Decision      `json:"decision"`
	Verdict       model.Verdict `json:"verdict"`
	Reason        string        `json:"reason,omitempty"`
	SystemMessage string        `json:"system_message,omitempty"`
	Checks        []CheckResult `json:"checks"`
}

func InconclusiveBlock(reason string) Result {
	return Result{
		SchemaVersion: model.SchemaVersion,
		Decision:      Block,
		Verdict:       model.Inconclusive,
		Reason:        reason,
		Checks:        []CheckResult{},
	}
}

func Evaluate(repo *repository.Repository, cfg model.Config) Result {
	return EvaluateWithBudget(repo, cfg, DefaultValidationBudget)
}

func EvaluateWithBudget(repo *repository.Repository, cfg model.Config, validationBudget time.Duration) Result {
	ctx, cancel := context.WithTimeout(context.Background(), validationBudget)
	defer cancel()

	result := Result{
		SchemaVersion: model.SchemaVersion,
		Decision:      Allow,
		Verdict:       model.Pass,
		Checks:        []CheckResult{},
	}
	var blockers, warnings []string
	fingerprintCache := evidence.NewFingerprintCache()
	ids := make([]string, 0, len(cfg.Checks))
	for id, check := range cfg.Checks {
		if check.IsRequired() {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			blockers = append(blockers, fmt.Sprintf("evidence validation exceeded the %s safety budget; narrow relevant_files or run `beforedone doctor`", validationBudget))
			break
		}

		checkResult := CheckResult{CheckID: id, Verdict: model.Inconclusive}
		receipt, err := evidence.LoadLatest(repo, id)
		if err != nil {
			checkResult.Reason = fmt.Sprintf("%s: no evidence receipt; run `beforedone check %s`", id, id)
			blockers = append(blockers, checkResult.Reason)
			result.Checks = append(result.Checks, checkResult)
			continue
		}
		checkResult.ReceiptVerdict = receipt.Verdict
		if err := evidence.VerifySignature(repo, receipt); err != nil {
			checkResult.Reason = fmt.Sprintf("%s: invalid evidence receipt (%v)", id, err)
			blockers = append(blockers, checkResult.Reason)
			result.Checks = append(result.Checks, checkResult)
			continue
		}

		switch receipt.Verdict {
		case model.Inconclusive:
			checkResult.Reason = fmt.Sprintf("%s: verification was INCONCLUSIVE (%s)", id, receipt.Error)
			warnings = append(warnings, checkResult.Reason)
			result.Checks = append(result.Checks, checkResult)
			continue
		case model.Fail:
			checkResult.Verdict = model.Fail
			checkResult.Reason = fmt.Sprintf("%s: latest verification FAILED; run `beforedone check %s` after fixing it", id, id)
			blockers = append(blockers, checkResult.Reason)
			result.Checks = append(result.Checks, checkResult)
			continue
		}

		fresh, reason := evidence.ValidateFreshContext(ctx, repo, cfg, receipt, fingerprintCache)
		checkResult.Fresh = boolPointer(fresh)
		if !fresh {
			checkResult.Reason = fmt.Sprintf("%s: evidence is stale (%s); run `beforedone check %s`", id, reason, id)
			blockers = append(blockers, checkResult.Reason)
			result.Checks = append(result.Checks, checkResult)
			continue
		}
		checkResult.Verdict = model.Pass
		result.Checks = append(result.Checks, checkResult)
	}

	if len(blockers) > 0 {
		result.Decision = Block
		result.Verdict = model.Inconclusive
		for _, check := range result.Checks {
			if check.Verdict == model.Fail {
				result.Verdict = model.Fail
				break
			}
		}
		result.Reason = "BeforeDone requires fresh evidence before completion:\n- " + strings.Join(blockers, "\n- ")
		return result
	}
	if len(warnings) > 0 {
		result.Verdict = model.Inconclusive
		result.SystemMessage = "BeforeDone could not reach a conclusive verdict:\n- " + strings.Join(warnings, "\n- ")
	}
	return result
}

func boolPointer(value bool) *bool {
	return &value
}
