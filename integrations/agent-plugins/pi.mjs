import { WorkerBridge } from './bridge.mjs';
import { scenarios } from './scenarios.mjs';

export default function openGuardrail(pi) {
  const bridges = new Map();
  for (const scenario of scenarios) {
    for (const tool of scenario.tools) {
      pi.registerTool({
        name: tool.name,
        label: tool.name.replaceAll('_', ' '),
        description: tool.description,
        parameters: tool.schema,
        async execute(_id, params, signal) {
          let bridge = bridges.get(scenario.id);
          if (!bridge) {
            bridge = new WorkerBridge(scenario);
            bridges.set(scenario.id, bridge);
          }
          const value = await bridge.request(tool.method, params, signal);
          return { content: [{ type: 'text', text: JSON.stringify(value) }], details: value };
        },
      });
    }
  }
  // pi 1.5 emits this for quit, reload, new, resume and fork. Each replacement
  // loads a fresh extension instance, so approvals never cross conversations.
  pi.on('session_shutdown', async () => {
    await Promise.all([...bridges.values()].map((bridge) => bridge.close()));
    bridges.clear();
  });
}
