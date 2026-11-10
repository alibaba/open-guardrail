# Live file cleanup demo

Run one cleanup task against a real OpenGuardrail service and a controlled file
tool. The browser shows the files on disk, each Run, the accepted evidence and
the tool's actual deletions. Change the initial targets or the batch limit and
run again to compare the outcome.

The browser runner imports the policy and guarded file operations from
[`scenarios/file-cleanup/`](../../scenarios/file-cleanup/README.md). This
directory contains the demonstration page and its entrypoint.

The runner is predefined Python code, not a model. No model account, Docker,
Jenkins or separate database is required.

## Start

From the checkout or native archive root, with Python 3.9+:

```sh
python3 examples/cleanup-demo/demo.py
```

A source checkout also needs the Go version in `go.mod`. The script builds both
commands from source; the first build may download dependencies. An archive uses
its native binaries. To reuse an existing build:

```sh
make build
python3 examples/cleanup-demo/demo.py --binaries "$PWD/bin"
```

Choose a password at the hidden prompts. The script starts an isolated SQLite
service and prints two loopback URLs:

- **Demo:** open this first. No login is required to run the limited cleanup
  task or inspect its Run details.
- **Console:** sign in as `demo` with the password chosen at startup. Select
  **Runs** to inspect the same Run IDs in the regular management UI. Another
  browser requires its own login.

Use the printed `127.0.0.1` URLs as-is. This local demo does not accept
`localhost` or other hostnames.

Keep the terminal open. Ctrl-C revokes the example's machine key, stops both
services and removes the temporary directory, credentials and SQLite data.
The machine key expires after one hour; restart the demo for a longer session.
For automation, `--password-stdin` reads the chosen password from standard input.
Do not put passwords in command-line arguments or commit them to the repository.
Use `--port 57743` if a fixed demo page URL is useful.

## Try it

The task panel combines file selection and rules. The result panel shows the
latest task's decisions and actual file changes. Runs and original evidence are
available under **History and evidence**.

1. Leave all four files selected and click **Run cleanup task**. The initial
   request includes orders and configuration. The runner submits the observed
   file metadata; OpenGuardrail stores a `BLOCKED / DENY` Run.
2. With automatic revision enabled, the runner removes protected targets from
   its request and checks again. The new `PASS / ALLOW` Run has its own ID.
   The deletion tool rereads that Run and its accepted evidence before deleting
   the two temporary files. Orders and configuration remain on disk.
3. Under **History and evidence**, expand the task, then **Inspect the actual
   Run, policy and accepted evidence**. These objects
   were read back from the service, including the frozen revision and final
   Submission. The policy is also shown as YAML. The original denial remains
   in the history.
4. Restore the sample files. Disable automatic revision and run the broad
   request: the result is a denial and all four files remain.
5. Select only the two `.tmp` files and set the limit to one. Without automatic
   revision the request is denied. Enable revision and run again: the runner
   narrows it to one temporary file, gets a new decision and deletes that file.
   The remaining temporary file can be cleaned by another task.

Restoring files creates a new workspace identity and directory. It preserves
previous task history; an earlier workspace's approval cannot authorize the new
one. The page retains the last ten tasks. All service Runs remain available in
the console until the demo stops.

## Responsibilities and limits

`Gateway` publishes the selected policy, creates a Run, reads its frozen revision,
checks actual file metadata and submits a complete Snapshot. The file count is
part of the published policy; changing it creates or selects the matching
revision. The service validates and records the assessment. It does not itself
inspect the filesystem or infer whether the submitted observations are true.

The policy source is the YAML template in
[`scenarios/file-cleanup/policy.yaml`](../../scenarios/file-cleanup/policy.yaml).
The runner replaces
`${max_files}` with the selected limit (1 or 2) before publishing. The console
and Run revision contain that rendered YAML verbatim. The demo verifies the
Run's frozen policy against the exact YAML it published; no Python YAML package
is required.

`FileTool.execute` rereads the authoritative Run and final Submission. It checks
the decision, expiry, revision, ChangeSet, accepted evidence and current file
metadata before unlinking the exact approved filenames. A missing response,
changed file, expired decision or mismatch stops execution. The tool never accepts
an arbitrary path. A refusal is fed back to the runner; at most one revised
request is attempted, with a separate Run.

Files live in a private disposable directory owned by this process. The tool
uses fixed basenames and directory-relative file descriptors, rejects symlinks
and hard links, and does not expose a shell or general filesystem endpoint.
The demo HTTP server binds to `127.0.0.1`, validates Host and Origin, and requires
a per-process write token. It exposes only this example's state, task and reset
operations, not the administrator or machine credentials.

This is a reference integration, not an OS sandbox or protection against another
process running as the same OS user. Deleting multiple files is not an atomic
filesystem transaction. A tool failure can leave a partially completed operation;
inspect the actual directory and records before retrying. Uncertain Run writes
are not automatically retried under fresh operation IDs. Stable IDs and private
inputs are retained until shutdown for inspection. See [Run recovery](../../docs/runs.md#run-retry-and-credential-recovery).

## Verify changes

```sh
python3 -m unittest discover -s examples/cleanup-demo -p 'test_*.py'
make build
CLEANUP_DEMO_BINARIES="$PWD/bin" python3 -m unittest discover \
  -s examples/cleanup-demo -p 'test_*.py' -v
```

The first command checks execution rejection paths using disposable files. The
second test invocation also starts a real service and verifies persisted Runs,
actual file effects, policy changes and the local HTTP boundary. Browser checks
should cover the default task, denial without revision, a changed batch limit,
Run inspection, reset and a narrow viewport.

The [Recovery contract self-check](../agent-demo/README.md) remains available
separately. For connecting another tool, start with the
[integration guide](../../integrations/README.md).
