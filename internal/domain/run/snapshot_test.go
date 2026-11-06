package run

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const submissionSpec = `{"apiVersion":"guardrail.open/v1alpha1","kind":"GuardrailSpec","metadata":{"name":"recovery"},"spec":{"description":"Check readiness","instructions":"Inspect current state","validForSeconds":900,"checks":[{"id":"readiness","displayName":"Readiness","description":"Ready","instructions":"Inspect","criteria":{"pass":"Healthy","block":"Unhealthy","unknown":"Unverified"},"evidenceRequirements":[{"id":"health","description":"Health state","appliesToVerdicts":["PASS","BLOCKED","UNKNOWN"]}]},{"id":"tests","displayName":"Tests","description":"Tests","instructions":"Inspect","criteria":{"pass":"Passed","block":"Failed","unknown":"Unverified"},"evidenceRequirements":[{"id":"report","description":"Test result","appliesToVerdicts":["PASS","BLOCKED"]}]}]}}`

func snapshotFixture() SubmissionSnapshot {
	observation := Observation{RequirementID: "health", ObservedAt: "2026-10-26T08:00:00+08:00", QuerySummary: "checked host health", ResultSummary: "healthy"}
	return SubmissionSnapshot{BaseSubmissionNo: 0, FinishRun: false, Verdict: "PASS", Summary: "all checks passed", Checks: []CheckSnapshot{
		{CheckID: "tests", Verdict: "PASS", Summary: "tests passed", Evidence: []Observation{{RequirementID: "report", ObservedAt: "2026-10-26T00:00:00Z", QuerySummary: "queried tests", ResultSummary: "passed"}}},
		{CheckID: "readiness", Verdict: "PASS", Summary: "healthy", Evidence: []Observation{observation, {RequirementID: "health", ObservedAt: "2026-10-26T00:01:00Z", QuerySummary: "checked readiness", ResultSummary: "healthy"}}},
	}}
}

func TestSnapshotScreensStructuredSecretsInEveryTextField(t *testing.T) {
	secrets := []string{
		`{"accessToken":"must-not-persist"}`,
		`{"x-api-key":"must-not-persist"}`,
		`{"dbPassword":"must-not-persist"}`,
		`{"authToken":"must-not-persist"}`,
		`{"userAccessToken":"must-not-persist"}`,
		`{"aliyunAccessKeyId":"must-not-persist"}`,
		`{"secretKey":"must-not-persist"}`,
		`{"awsSecretAccessKey":"must-not-persist"}`,
		`{"credentials":{"value":"must-not-persist"}}`,
		`{"observations":[{"serviceAccountKey":"must-not-persist"}]}`,
		`{"access\u0054oken":"must-not-persist"}`,
		`{"accessToken":"must-not-persist","accessToken":""}`,
		`ogb_abcdefghijklmnop`,
	}
	encoded, err := json.Marshal(`{"dbPassword":"must-not-persist"}`)
	if err != nil {
		t.Fatal(err)
	}
	secrets = append(secrets, string(encoded))
	for _, field := range []string{"summary", "check summary", "query", "result", "reference"} {
		t.Run(field, func(t *testing.T) {
			for i, secret := range secrets {
				input := snapshotFixture()
				switch field {
				case "summary":
					input.Summary = secret
				case "check summary":
					input.Checks[1].Summary = secret
				case "query":
					input.Checks[1].Evidence[0].QuerySummary = secret
				case "result":
					input.Checks[1].Evidence[0].ResultSummary = secret
				case "reference":
					input.Checks[1].Evidence[0].Reference = &secret
				}
				if _, err := EvaluateSnapshot(submissionSpec, input); !errors.Is(err, ErrSensitiveEvidence) {
					t.Fatalf("secret case %d was accepted: %v", i, err)
				}
			}
		})
	}
}

func TestSnapshotKeepsBenignObservationTextAndDigest(t *testing.T) {
	for _, observation := range []string{
		`{"ready":true,"replicas":3,"tokenCount":42,"accessToken":""}`,
		`{"observations":[{"healthy":true}],"credentialCount":0}`,
		`[INFO] health probe passed`,
		`"healthy"`,
	} {
		input := snapshotFixture()
		input.Checks[1].Evidence[0].ResultSummary = observation
		first, err := EvaluateSnapshot(submissionSpec, input)
		if err != nil || first.Snapshot.Checks[0].Evidence[0].ResultSummary != observation {
			t.Fatalf("benign observation changed or rejected: %v", err)
		}
		replay, err := EvaluateSnapshot(submissionSpec, first.Snapshot)
		if err != nil || replay.RequestDigest != first.RequestDigest {
			t.Fatalf("screening changed retry identity: %v", err)
		}
	}
}
func TestEvaluateStructuredSnapshot(t *testing.T) {
	input := snapshotFixture()
	input.Summary = ""
	result, err := EvaluateSnapshot(submissionSpec, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Verdict != "PASS" || result.Decision != "ALLOW" || len(result.EvidenceDigests[0]) != 2 || result.Snapshot.Checks[0].CheckID != "readiness" {
		t.Fatalf("result=%+v", result)
	}
	if result.Snapshot.Checks[0].Evidence[0].ObservedAt != "2026-10-26T00:00:00Z" {
		t.Fatal("timestamp not normalized")
	}
	if result.Snapshot.Summary != "" || !strings.Contains(result.CanonicalJSON, `"summary":""`) {
		t.Fatal("empty Submission summary was changed or omitted")
	}
	again, err := EvaluateSnapshot(submissionSpec, result.Snapshot)
	if err != nil || again.RequestDigest != result.RequestDigest {
		t.Fatalf("replay changed digest: %v", err)
	}
}
func TestSnapshotDeniedDecisions(t *testing.T) {
	for _, tc := range []struct {
		name      string
		readiness string
		tests     string
		verdict   string
	}{
		{"blocked check", "BLOCKED", "PASS", "BLOCKED"},
		{"unknown check", "UNKNOWN", "PASS", "UNKNOWN"},
		{"blocked overrides unknown", "UNKNOWN", "BLOCKED", "BLOCKED"},
		{"unknown does not override blocked", "BLOCKED", "UNKNOWN", "BLOCKED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := snapshotFixture()
			input.Verdict = tc.verdict
			input.Checks[1].Verdict = tc.readiness
			input.Checks[0].Verdict = tc.tests
			if tc.tests == "UNKNOWN" {
				// The report requirement only applies to PASS and BLOCKED.
				input.Checks[0].Evidence = nil
			}
			result, err := EvaluateSnapshot(submissionSpec, input)
			if err != nil {
				t.Fatal(err)
			}
			if result.Verdict != tc.verdict || result.Decision != "DENY" {
				t.Fatalf("verdict=%s decision=%s", result.Verdict, result.Decision)
			}
		})
	}
}

func TestEvaluateStructuredSnapshotRejectsInvalid(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*SubmissionSnapshot)
		want   error
	}{
		{"verdict mismatch", func(s *SubmissionSnapshot) { s.Verdict = "BLOCKED" }, ErrVerdictMismatch},
		{"duplicate evidence", func(s *SubmissionSnapshot) {
			s.Checks[1].Evidence = append(s.Checks[1].Evidence, s.Checks[1].Evidence[0])
		}, ErrInvalidSubmission},
		{"inapplicable evidence", func(s *SubmissionSnapshot) { s.Checks[0].Verdict = "UNKNOWN" }, ErrInvalidSubmission},
		{"missing evidence", func(s *SubmissionSnapshot) { s.Checks[1].Evidence = nil }, ErrInvalidSubmission},
		{"oversize summary", func(s *SubmissionSnapshot) { s.Summary = strings.Repeat("界", MaxSummaryRunes+1) }, ErrInvalidSubmission},
		{"whitespace summary", func(s *SubmissionSnapshot) { s.Summary = " \t\n" }, ErrInvalidSubmission},
		{"invalid UTF-8 summary", func(s *SubmissionSnapshot) { s.Summary = string([]byte{0xff}) }, ErrInvalidSubmission},
		{"empty check summary", func(s *SubmissionSnapshot) { s.Checks[1].Summary = "" }, ErrInvalidSubmission},
		{"empty evidence query", func(s *SubmissionSnapshot) { s.Checks[1].Evidence[0].QuerySummary = "" }, ErrInvalidSubmission},
		{"empty evidence result", func(s *SubmissionSnapshot) { s.Checks[1].Evidence[0].ResultSummary = "" }, ErrInvalidSubmission},
		{"invalid timestamp", func(s *SubmissionSnapshot) { s.Checks[1].Evidence[0].ObservedAt = "yesterday" }, ErrInvalidSubmission},
		{"sensitive observation", func(s *SubmissionSnapshot) {
			s.Checks[1].Evidence[0].ResultSummary = "Bearer abcdefghijklmnopqrstuvwxyz"
		}, ErrSensitiveEvidence},
		{"administrator session in summary", func(s *SubmissionSnapshot) {
			s.Summary = "observed ogs_abcdefghijklmnop"
		}, ErrSensitiveEvidence},
		{"Run token in evidence", func(s *SubmissionSnapshot) {
			s.Checks[1].Evidence[0].ResultSummary = "observed ogrt_abcdefghijklmnop"
		}, ErrSensitiveEvidence},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := snapshotFixture()
			tc.mutate(&v)
			_, err := EvaluateSnapshot(submissionSpec, v)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}
