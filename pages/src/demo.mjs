// The website illustration is deliberately separate from the live service.
export const files = ['cache.tmp', 'download.tmp', 'orders.csv', 'config.yaml'];

export function assessRequest(targets, evidenceAvailable = true) {
  const selected = [...new Set(targets)];
  const knownTargets = selected.every(file => files.includes(file));
  const temporaryOnly = selected.length > 0 && selected.every(file => file.endsWith('.tmp'));
  const batchWithinLimit = selected.length > 0 && selected.length <= 2;
  if (!evidenceAvailable || !knownTargets || !selected.length) {
    return { result: 'UNKNOWN', decision: 'DENY', checks: ['UNKNOWN', 'UNKNOWN'], reason: 'unknown' };
  }
  const checks = [temporaryOnly ? 'PASS' : 'BLOCKED', batchWithinLimit ? 'PASS' : 'BLOCKED'];
  return {
    result: checks.includes('BLOCKED') ? 'BLOCKED' : 'PASS',
    decision: checks.includes('BLOCKED') ? 'DENY' : 'ALLOW',
    checks,
    reason: !temporaryOnly ? 'protected' : !batchWithinLimit ? 'batch' : 'allowed',
  };
}
