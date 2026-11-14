// Each published guide must have both an English and a Chinese source.
export const docs = [
  ['overview', 'docs/product-overview.md', 'Product overview', '产品介绍', 0, 'docs/i18n/product-overview.zh-CN.md'],
  ['getting-started', 'docs/getting-started.md', 'Getting started', '快速开始', 0, 'docs/i18n/getting-started.zh-CN.md'],
  ['native-install', 'docs/native-install.md', 'Native installation', '下载安装', 0, 'docs/i18n/native-install.zh-CN.md'],
  ['plugin-trial', 'docs/plugin-trial.md', 'Agent plugin', 'Agent 插件', 0, 'docs/plugin-trial.zh-CN.md'],
  ['concepts', 'docs/concepts.md', 'Core concepts', '核心概念', 0, 'docs/i18n/concepts.zh-CN.md'],
  ['guardrailspec-reference', 'docs/guardrailspec-reference.md', 'GuardrailSpec reference', 'GuardrailSpec 参考', 1, 'docs/i18n/guardrailspec-reference.zh-CN.md'],
  ['scenes-and-specs', 'docs/scenes-and-specs.md', 'Scenes and policies', '场景与策略', 1, 'docs/i18n/scenes-and-specs.zh-CN.md'],
  ['runs', 'docs/runs.md', 'Runs and submissions', 'Run 与 Submission', 1, 'docs/i18n/runs.zh-CN.md'],
  ['resource-bindings', 'examples/resource-bindings/README.md', 'Resource bindings', '资源绑定', 1, 'examples/resource-bindings/README.zh-CN.md'],
  ['api', 'docs/api.md', 'HTTP API', 'HTTP API', 1, 'docs/i18n/api.zh-CN.md'],
  ['cli', 'cmd/guardrailctl/README.md', 'guardrailctl CLI', 'guardrailctl CLI', 1, 'docs/i18n/cli.zh-CN.md'],
  ['configuration', 'docs/configuration.md', 'Configuration and storage', '配置与存储', 2, 'docs/i18n/configuration.zh-CN.md'],
  ['docker', 'docs/docker.md', 'Docker deployment', 'Docker 部署', 2, 'docs/i18n/docker.zh-CN.md'],
  ['local-administrator', 'docs/local-administrator.md', 'Administrator guide', '管理员指南', 2, 'docs/i18n/local-administrator.zh-CN.md'],
  ['integrations', 'integrations/README.md', 'Integration guide', '集成指南', 2, 'docs/i18n/integrations.zh-CN.md'],
  ['architecture', 'docs/architecture.md', 'Architecture', '架构', 2, 'docs/i18n/architecture.zh-CN.md'],
  ['adoption-guide', 'docs/adoption-guide.md', 'Plan your integration', '接入指引', 2, 'docs/i18n/adoption-guide.zh-CN.md'],
];

// Contributor-only translations are checked, but do not add product-site routes.
export const contributorDocs = [
  ['development-roadmap', 'docs/development-roadmap.md', 'Development roadmap', '开发路线图', null, 'docs/i18n/development-roadmap.zh-CN.md'],
];

export function headingId(text) {
  return text.toLowerCase().replace(/<[^>]+>/g, '').replace(/&[^;]+;/g, '')
    .replace(/[^\p{L}\p{N}\s_-]/gu, '').replace(/\s+/g, '-');
}
