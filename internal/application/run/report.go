package run

import (
	"context"
	"encoding/json"
)

// Report contains structured diagnostics and content fingerprints suitable for
// export. Business context, YAML and free-text observations stay in their readers.
type ReportRevision struct {
	ID             string `json:"id"`
	PolicyID       string `json:"policyId"`
	Digest         string `json:"digest"`
	RevisionNumber *int64 `json:"revisionNumber,omitempty"`
	PublishedAt    *int64 `json:"publishedAt,omitempty"`
}

type Report struct {
	Revision        ReportRevision     `json:"revision"`
	Format          string             `json:"format"`
	GeneratedAt     int64              `json:"generatedAt"`
	Run             PolicyRunSummary   `json:"run"`
	Reasons         []string           `json:"reasons"`
	Submissions     []ReportSubmission `json:"submissions"`
	NextBefore      *int64             `json:"nextBefore,omitempty"`
	FinalSubmission *ReportSubmission  `json:"finalSubmission,omitempty"`
}
type ReportSubmission struct {
	ID             string        `json:"id"`
	SubmissionNo   int64         `json:"submissionNo"`
	Verdict        string        `json:"verdict"`
	FinishRun      bool          `json:"finishRun"`
	CreatedAt      int64         `json:"createdAt"`
	SnapshotDigest string        `json:"snapshotDigest,omitempty"`
	Checks         []ReportCheck `json:"checks,omitempty"`
}
type ReportCheck struct {
	CheckID  string           `json:"checkId"`
	Verdict  string           `json:"verdict"`
	Evidence []ReportEvidence `json:"evidence"`
}
type ReportEvidence struct {
	RequirementID   string  `json:"requirementId"`
	ObservedAt      string  `json:"observedAt"`
	QueryDigest     string  `json:"queryDigest"`
	ResultDigest    string  `json:"resultDigest"`
	ReferenceDigest *string `json:"referenceDigest,omitempty"`
}

func (s *Service) Report(ctx context.Context, scope Scope, id string) (Report, error) {
	current, err := s.Get(ctx, scope, id)
	if err != nil {
		return Report{}, err
	}
	// Initial Run state bounds the history. Concurrent newer submissions are
	// available on the next refresh, rather than mixed into this captured state.
	page, err := s.ListRunSubmissions(ctx, scope, id, 20, current.LatestSubmissionNo+1)
	if err != nil {
		return Report{}, err
	}
	revision, err := s.GetRunPolicyRevision(ctx, scope, id)
	if err != nil {
		return Report{}, err
	}
	if revision.Status != "PUBLISHED" || revision.ID != current.PolicyRevisionID || revision.PolicyID != current.PolicyID || revision.Digest == nil || *revision.Digest != current.SpecDigest {
		return Report{}, ErrUnavailable
	}
	report := Report{Format: "openguardrail.run-report.v1", GeneratedAt: s.nowUnix(), Run: current.Summary(), Reasons: []string{}, Submissions: []ReportSubmission{}, NextBefore: page.NextBefore}
	report.Revision = ReportRevision{ID: revision.ID, PolicyID: revision.PolicyID, Digest: *revision.Digest, RevisionNumber: revision.RevisionNumber, PublishedAt: revision.PublishedAt}
	switch {
	case current.Status == "OPEN":
		report.Reasons = append(report.Reasons, "RUN_OPEN")
	case current.Status == "EXPIRED":
		report.Reasons = append(report.Reasons, "RUN_EXPIRED")
	case current.DecisionExpiresAt != nil && report.GeneratedAt >= *current.DecisionExpiresAt:
		report.Reasons = append(report.Reasons, "DECISION_EXPIRED")
	}
	for _, item := range page.Items {
		report.Submissions = append(report.Submissions, reportSubmission(item))
	}
	if current.FinalSubmissionID != nil {
		accepted, err := s.GetRunSubmission(ctx, scope, id, *current.FinalSubmissionID)
		if err != nil {
			return Report{}, err
		}
		if accepted.ID != *current.FinalSubmissionID || accepted.RunID != current.ID || !accepted.FinishRun {
			return Report{}, ErrUnavailable
		}
		final := reportSubmission(accepted.SnapshotSubmission)
		raw, err := json.Marshal(accepted.Snapshot)
		if err != nil {
			return Report{}, ErrUnavailable
		}
		final.SnapshotDigest = hashString(string(raw))
		final.Checks = []ReportCheck{}
		blocked, unknown := false, false
		for _, check := range accepted.Snapshot.Checks {
			blocked = blocked || check.Verdict == "BLOCKED"
			unknown = unknown || check.Verdict == "UNKNOWN"
			record := ReportCheck{CheckID: check.CheckID, Verdict: check.Verdict, Evidence: []ReportEvidence{}}
			for _, e := range check.Evidence {
				evidence := ReportEvidence{RequirementID: e.RequirementID, ObservedAt: e.ObservedAt, QueryDigest: hashString(e.QuerySummary), ResultDigest: hashString(e.ResultSummary)}
				if e.Reference != nil {
					digest := hashString(*e.Reference)
					evidence.ReferenceDigest = &digest
				}
				record.Evidence = append(record.Evidence, evidence)
			}
			final.Checks = append(final.Checks, record)
		}
		if blocked {
			report.Reasons = append(report.Reasons, "CHECK_BLOCKED")
		}
		if unknown {
			report.Reasons = append(report.Reasons, "CHECK_UNKNOWN")
		}
		report.FinalSubmission = &final
	}
	return report, nil
}
func reportSubmission(s SnapshotSubmission) ReportSubmission {
	return ReportSubmission{ID: s.ID, SubmissionNo: s.SubmissionNo, Verdict: s.Verdict, FinishRun: s.FinishRun, CreatedAt: s.CreatedAt}
}
