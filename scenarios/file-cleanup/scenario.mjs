// Host-neutral definition. The pi and DSH adapters register these tools.
const string = { type: 'string' };
const targets = { type: 'array', items: string, maxItems: 4, uniqueItems: true };
const object = (properties, required = Object.keys(properties)) => ({ type: 'object', properties, required, additionalProperties: false });

export default {
  id: 'file-cleanup',
  workerUrl: new URL('./worker.py', import.meta.url),
  skill: {
    name: 'guardrail-cleanup',
    url: new URL('./skills/guardrail-cleanup/SKILL.md', import.meta.url),
    description: 'Assess and execute isolated cleanup using frozen policy and evidence',
  },
  tools: [
    { method: 'status', name: 'guardrail_cleanup_status', description: 'Check the isolated service, actual files and the latest ten authoritative Run summaries. Supply runId to read one owned Run with its frozen revision and accepted evidence. Never touches an existing service or user files.', schema: object({ runId: string }, []) },
    { method: 'prepare', name: 'guardrail_cleanup_prepare', description: 'Freeze exact proposed sample filenames into a Run. Returns frozen policy and tool-observed file metadata, without choosing verdicts. Use a stable requestId; a changed request needs a new ID.', schema: object({ requestId: string, targets }) },
    { method: 'submit', name: 'guardrail_cleanup_submit', description: 'Submit your own verdict and summary for both frozen policy checks (temporary-only and batch-limit). Evidence comes from the tool observation frozen at prepare. PASS, BLOCKED or UNKNOWN; no file is deleted.', schema: object({ runId: string, checks: { type: 'array', minItems: 2, maxItems: 2, items: object({ checkId: string, verdict: { type: 'string', enum: ['PASS', 'BLOCKED', 'UNKNOWN'] }, summary: string }) } }) },
    { method: 'execute', name: 'guardrail_cleanup_execute', description: 'Execute an assessed cleanup Run on exactly its frozen targets. Independently verifies authoritative ALLOW, validity, accepted evidence, actual file state and business rules. A refusal deletes nothing; inspect status after any execution error.', schema: object({ runId: string, targets }) },
  ],
};
