import { CleanupBridge, tools } from './bridge.mjs';

export default function openGuardrail(pi) {
  let bridge;
  for (const tool of tools) {
    pi.registerTool({
      name: tool.name,
      label: tool.name.replaceAll('_', ' '),
      description: tool.description,
      parameters: tool.schema,
      async execute(_id, params, signal) {
        bridge ||= new CleanupBridge();
        const value = await bridge.request(tool.method, params, signal);
        return { content: [{ type: 'text', text: JSON.stringify(value) }], details: value };
      },
    });
  }
  // pi 1.5 emits this for quit, reload, new, resume and fork. Each replacement
  // loads a fresh extension instance, so approvals never cross conversations.
  pi.on('session_shutdown', async () => { await bridge?.close(); });
}
