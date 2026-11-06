package storage

func fixedRunTime(seconds int64) func() int64 { return func() int64 { return seconds } }

const sampleSpec = `apiVersion: guardrail.open/v1alpha1
kind: GuardrailSpec
metadata:
  name: recovery
spec:
  description: Check recovery readiness
  instructions: Inspect current state and provide evidence
  validForSeconds: 900
  checks:
    - id: readiness
      displayName: Recovery readiness
      description: Check recovery readiness
      instructions: Inspect current health and provide evidence
      criteria:
        pass: Health is good
        block: Health is bad
        unknown: Health is not verified
      evidenceRequirements:
        - id: health
          description: Record current health
          appliesToVerdicts: [PASS, BLOCKED, UNKNOWN]
`
