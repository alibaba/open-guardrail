import { spawn } from 'node:child_process';
import { createInterface } from 'node:readline';
import { fileURLToPath } from 'node:url';

// One private worker per loaded plugin. It owns only a disposable service and
// four fixed sample files. No shell command or user-supplied executable is run.
export class CleanupBridge {
  #child;
  #pending = new Map();
  #next = 0;
  #closed = false;
  #failed = false;
  #initialized = false;
  #stopping;
  #tail = Promise.resolve();

  request(method, params = {}, signal) {
    const run = () => this.#request(method, params, signal);
    const result = this.#tail.then(run, run);
    this.#tail = result.catch(() => {});
    return result;
  }

  #request(method, params, signal) {
    if (this.#closed || this.#failed) return Promise.reject(new Error('Cleanup session stopped; inspect prior outcomes before starting a new session'));
    if (signal?.aborted) return Promise.reject(new Error('Cleanup call cancelled before admission'));
    if (!this.#child) {
      this.#child = spawn(process.env.OPEN_GUARDRAIL_PYTHON || 'python3',
        ['-B', fileURLToPath(new URL('./worker.py', import.meta.url))],
        { stdio: ['pipe', 'pipe', 'ignore'], env: process.env });
      const lines = createInterface({ input: this.#child.stdout });
      lines.on('line', (line) => {
        if (this.#failed) return;
        try {
          if (line.length > 1024 * 1024) throw new Error('response too large');
          const reply = JSON.parse(line);
          const pending = this.#pending.get(reply.id);
          if (!pending) throw new Error('uncorrelated response');
          this.#initialized = reply.initialized === true;
          this.#pending.delete(reply.id);
          pending.finish();
          if (reply.error) pending.reject(new Error(`${reply.error.code}: ${reply.error.message}`));
          else pending.resolve(reply.value);
        } catch {
          this.#stop('Cleanup response could not be verified; execution stopped');
        }
      });
      this.#child.on('error', () => this.#stop('Cleanup worker unavailable; check Python and native binaries'));
      this.#child.on('exit', () => this.#stop('Cleanup worker stopped; do not retry uncertain execution automatically'));
      this.#child.stdin.on('error', () => this.#stop('Cleanup worker input closed'));
    }
    return new Promise((resolve, reject) => {
      const id = ++this.#next;
      const abort = () => this.#stop('Cleanup call cancelled; inspect actual outcomes before retrying');
      // Startup can spend 30s waiting for readiness and 5 * 40s in setup
      // commands. A first prepare adds 3 * 40s; allow 70s of scheduling margin.
      const timeout = this.#initialized ? 90_000 : 420_000;
      const timer = setTimeout(() => this.#stop('Cleanup call timed out; inspect actual outcomes before retrying'), timeout);
      this.#pending.set(id, { resolve, reject, finish: () => {
        clearTimeout(timer);
        signal?.removeEventListener('abort', abort);
      } });
      signal?.addEventListener('abort', abort, { once: true });
      if (signal?.aborted) return abort();
      this.#child.stdin.write(JSON.stringify({ id, method, params }) + '\n');
    });
  }

  #stop(message) {
    if (this.#stopping) return this.#stopping;
    this.#failed = true;
    const rejectPending = () => {
      for (const pending of this.#pending.values()) {
        pending.finish();
        pending.reject(new Error(message));
      }
      this.#pending.clear();
    };
    this.#stopping = new Promise((resolve) => {
      const child = this.#child;
      if (!child?.pid || child.exitCode !== null || child.signalCode !== null) {
        rejectPending(); resolve(); return;
      }
      // Drain the owned worker/service before reporting cancellation to a host.
      // The worker's SIGTERM handler always runs its service/file cleanup.
      child.once('exit', () => { rejectPending(); resolve(); });
      child.kill('SIGTERM');
    });
    return this.#stopping;
  }

  async close() {
    this.#closed = true;
    await this.#stop('Cleanup session closed');
  }
}

const string = { type: 'string' };
const targets = { type: 'array', items: string, maxItems: 4, uniqueItems: true };
const object = (properties, required = Object.keys(properties)) => ({ type: 'object', properties, required, additionalProperties: false });
export const tools = [
  { method: 'status', name: 'guardrail_cleanup_status', description: 'Check the isolated service, actual files and the latest ten authoritative Run summaries. Supply runId to read one owned Run with its frozen revision and accepted evidence. Never touches an existing service or user files.', schema: object({ runId: string }, []) },
  { method: 'prepare', name: 'guardrail_cleanup_prepare', description: 'Freeze exact proposed sample filenames into a Run. Returns frozen policy and tool-observed file metadata, without choosing verdicts. Use a stable requestId; a changed request needs a new ID.', schema: object({ requestId: string, targets }) },
  { method: 'submit', name: 'guardrail_cleanup_submit', description: 'Submit your own verdict and summary for both frozen policy checks (temporary-only and batch-limit). Evidence comes from the tool observation frozen at prepare. PASS, BLOCKED or UNKNOWN; no file is deleted.', schema: object({ runId: string, checks: { type: 'array', minItems: 2, maxItems: 2, items: object({ checkId: string, verdict: { type: 'string', enum: ['PASS', 'BLOCKED', 'UNKNOWN'] }, summary: string }) } }) },
  { method: 'execute', name: 'guardrail_cleanup_execute', description: 'Execute an assessed cleanup Run on exactly its frozen targets. Independently verifies authoritative ALLOW, validity, accepted evidence, actual file state and business rules. A refusal deletes nothing; inspect status after any execution error.', schema: object({ runId: string, targets }) },
];
