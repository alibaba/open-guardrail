---
name: guardrail-cleanup
description: Assess and execute the isolated file cleanup scenario using frozen policy and actual tool-observed evidence; applies to the four disposable sample files, not arbitrary user files.
---

Use `guardrail_cleanup_status` to check the service and actual files. The plugin
owns a disposable directory; operations never accept arbitrary paths. It creates
an isolated service, not a connection to your existing administrator session.

For the user's requested sample files:
1. Call `guardrail_cleanup_prepare` with a stable `requestId` and exact targets.
2. Read its **frozen policy** and observation. Independently assess each check,
   using `PASS`, `BLOCKED` or `UNKNOWN` with a reason. Do not infer a verdict from
   filenames alone when the observation or policy says otherwise.
3. Call `guardrail_cleanup_submit` with both check IDs, verdicts and summaries.
   The plugin attaches the exact frozen tool observation as evidence. Never
   invent observations or report a check as passing without supporting evidence.
4. Call `guardrail_cleanup_execute` with that Run ID and the same targets.
   The execution tool rereads the authoritative Run and evidence and independently
   checks actual state. A server ALLOW can still be refused by this tool.
5. On a policy denial, explain it. If the user's goal permits narrower scope,
   prepare a new Run under a new requestId, assess it and retry. Preserve the
   original denied Run. Do not retry uncertain writes or execution under fresh IDs.
6. Read status after execution, then pass each Run's `runId` to status to inspect
   its authoritative accepted evidence. The default history is a summary list.
   Report both Run IDs, actual decisions, deleted
   files and surviving files. Decisions authorize checks; they are not execution
   receipts. On cancellation or partial failure inspect outcomes before retrying.

For a demonstration, first assess all four sample files, then narrow to the
permitted targets after denial. Use only the plugin tools for this workflow.
Do not use shell, write tools or direct filesystem access to bypass a refusal.
This workflow protects the plugin's own operation, not every host tool or the OS.
