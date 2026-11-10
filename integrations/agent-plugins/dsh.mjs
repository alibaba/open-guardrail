import { WorkerBridge } from './bridge.mjs';
import { scenarios } from './scenarios.mjs';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

export const name = 'open-guardrail-scenarios';
export const inject = ['tools'];

export function apply(ctx) {
  const bridges = new Map();
  ctx.inject(['skills'], (context) => {
    for (const scenario of scenarios) {
      if (!scenario.skill) continue;
      const path = fileURLToPath(scenario.skill.url);
      const skill = readFileSync(path, 'utf8');
      context.skills.register({ name: scenario.skill.name, source: 'bundled', path,
        description: skill.match(/^description: (.+)$/m)?.[1] ?? scenario.skill.description,
        content: skill.split('\n---\n').slice(1).join('\n---\n').trim() });
    }
  });
  for (const scenario of scenarios) {
    for (const tool of scenario.tools) {
      ctx.tools.register({
        name: tool.name,
        description: tool.description,
        parameters: tool.schema,
        output: {
          schema: { type: 'object', additionalProperties: true },
          render: (_args, value) => [{ type: 'text', text: JSON.stringify(value) }],
        },
        async execute(params, exec) {
          if (!exec.agent) throw new Error('Scenario requires an owning agent session');
          let owned = bridges.get(exec.agent);
          if (!owned) { owned = new Map(); bridges.set(exec.agent, owned); }
          let bridge = owned.get(scenario.id);
          if (!bridge) { bridge = new WorkerBridge(scenario); owned.set(scenario.id, bridge); }
          return bridge.request(tool.method, params, exec.signal);
        },
      });
    }
  }
  ctx.on('agent/disposed', async ({ agent }) => {
    const owned = bridges.get(agent);
    await Promise.all([...(owned?.values() ?? [])].map((bridge) => bridge.close()));
    bridges.delete(agent);
  });
  ctx.effect(() => async () => {
    await Promise.all([...bridges.values()].flatMap((owned) => [...owned.values()].map((bridge) => bridge.close())));
    bridges.clear();
  });
}
