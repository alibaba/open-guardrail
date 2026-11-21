package run

import (
	"context"
	"github.com/alibaba/open-guardrail/internal/application/spec"
)

func (s *Service) checkRead(scope Scope, runID string) error {
	if scope.SubmissionCredentialHash != "" && scope.ActorID != "run-token:"+runID {
		return ErrUnauthorized
	}
	return nil
}

func (s *Service) GetRunPolicyRevision(ctx context.Context, scope Scope, runID string) (spec.PolicyRevision, error) {
	if err := s.checkRead(scope, runID); err != nil {
		return spec.PolicyRevision{}, err
	}
	return s.repository.GetRunPolicyRevision(ctx, scope, runID)
}
func (s *Service) GetRunSubmission(ctx context.Context, scope Scope, runID, submissionID string) (StoredSubmission, error) {
	if err := s.checkRead(scope, runID); err != nil {
		return StoredSubmission{}, err
	}
	return s.repository.GetRunSubmission(ctx, scope, runID, submissionID)
}
func (s *Service) ListRunSubmissions(ctx context.Context, scope Scope, runID string, limit int, before int64) (SubmissionPage, error) {
	if err := s.checkRead(scope, runID); err != nil {
		return SubmissionPage{}, err
	}
	return s.repository.ListRunSubmissions(ctx, scope, runID, limit, before)
}
