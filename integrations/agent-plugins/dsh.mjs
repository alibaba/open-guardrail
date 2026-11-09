import { CleanupBridge, tools } from './bridge.mjs';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

export const name = 'open-guardrail-cleanup';
export const inject = ['tools'];

export function apply(ctx) {
  const bridges = new Map();
  ctx.inject(['skills'], (context) => {
    const path = fileURLToPath(new URL('./skills/guardrail-cleanup/SKILL.md', import.meta.url));
    const skill = readFileSync(path, 'utf8');
    context.skills.register({ name: 'guardrail-cleanup', source: 'bundled', path,
      description: skill.match(/^description: (.+)$/m)?.[1] ?? 'Assess and execute isolated cleanup using frozen policy and evidence',
      content: skill.split('\n---\n').slice(1).join('\n---\n').trim() });
  });
  for (const tool of tools) {
    ctx.tools.register({
      name: tool.name,
      description: tool.description,
      parameters: tool.schema,
      output: {
        schema: { type: 'object', additionalProperties: true },
        render: (_args, value) => [{ type: 'text', text: JSON.stringify(value) }],
      },
      async execute(params, exec) {
        if (!exec.agent) throw new Error('Cleanup requires an owning agent session');
        let bridge = bridges.get(exec.agent);
        if (!bridge) { bridge = new CleanupBridge(); bridges.set(exec.agent, bridge); }
        return bridge.request(tool.method, params, exec.signal);
      },
    });
  }
  ctx.on('agent/disposed', async ({ agent }) => {
    const bridge = bridges.get(agent);
    await bridge?.close();
    bridges.delete(agent);
  });
  ctx.effect(() => async () => {
    await Promise.all([...bridges.values()].map((bridge) => bridge.close()));
    bridges.clear();
  });
}
