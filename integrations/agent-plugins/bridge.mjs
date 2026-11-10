import { spawn } from 'node:child_process';
import { createInterface } from 'node:readline';
import { fileURLToPath } from 'node:url';

// One serial worker per scenario and host session. The scenario owns its
// operation; this transport never interprets policy or executes host tools.
export class WorkerBridge {
  #scenario;
  #child;
  #pending = new Map();
  #next = 0;
  #closed = false;
  #failed = false;
  #initialized = false;
  #stopping;
  #tail = Promise.resolve();

  constructor(scenario) {
    this.#scenario = scenario;
  }

  request(method, params = {}, signal) {
    const run = () => this.#request(method, params, signal);
    const result = this.#tail.then(run, run);
    this.#tail = result.catch(() => {});
    return result;
  }

  #request(method, params, signal) {
    if (this.#closed || this.#failed) return Promise.reject(new Error('Scenario session stopped; inspect prior outcomes before starting a new session'));
    if (signal?.aborted) return Promise.reject(new Error('Scenario call cancelled before admission'));
    if (!this.#child) {
      this.#child = spawn(process.env.OPEN_GUARDRAIL_PYTHON || 'python3',
        ['-B', fileURLToPath(this.#scenario.workerUrl)],
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
          this.#stop('Scenario response could not be verified; execution stopped');
        }
      });
      this.#child.on('error', () => this.#stop('Scenario worker unavailable; check Python and native binaries'));
      this.#child.on('exit', () => this.#stop('Scenario worker stopped; do not retry uncertain execution automatically'));
      this.#child.stdin.on('error', () => this.#stop('Scenario worker input closed'));
    }
    return new Promise((resolve, reject) => {
      const id = ++this.#next;
      const abort = () => this.#stop('Scenario call cancelled; inspect actual outcomes before retrying');
      // Startup can spend 30s waiting for readiness and 5 * 40s in setup
      // commands. A first prepare adds 3 * 40s; allow 70s of scheduling margin.
      const timeout = this.#initialized ? 90_000 : 420_000;
      const timer = setTimeout(() => this.#stop('Scenario call timed out; inspect actual outcomes before retrying'), timeout);
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
    await this.#stop('Scenario session closed');
  }
}
