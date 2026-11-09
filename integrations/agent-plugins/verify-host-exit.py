"""Run a configured host and record natural exit independently of its exit code."""
import argparse
import json
import math
from pathlib import Path
import subprocess
import time


def verify(command, directory, timeout):
    directory.mkdir(mode=0o700, parents=True, exist_ok=False)
    terminated = False
    killed = False
    started = time.monotonic()
    with (directory / "stdout.jsonl").open("w") as output, (directory / "stderr.log").open("w") as errors:
        process = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=output, stderr=errors)
        try:
            process.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            terminated = True
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                killed = True
                process.kill()
                process.wait()
        finally:
            if process.poll() is None:
                terminated = True
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    killed = True
                    process.kill()
                    process.wait()
    result = {
        "returnCode": process.returncode,
        "supervisorTerminated": terminated,
        "supervisorKilled": killed,
        "normalHostExit": process.returncode == 0 and not terminated,
        "elapsedSeconds": round(time.monotonic() - started, 3),
    }
    (directory / "exit.json").write_text(json.dumps(result, indent=2) + "\n")
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--timeout", type=float, default=180)
    parser.add_argument("--output-dir", type=Path, required=True, help="new private directory for host logs")
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command[1:] if args.command[:1] == ["--"] else args.command
    if not command or not math.isfinite(args.timeout) or args.timeout <= 0:
        parser.error("a host command and positive timeout are required")
    result = verify(command, args.output_dir, args.timeout)
    print(json.dumps(result))
    return 0 if result["normalHostExit"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
