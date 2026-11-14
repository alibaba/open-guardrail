export const copy = {
  "en": {
    "lang": "en",
    "other": "简体中文",
    "title": "OpenGuardrail — Evidence before action",
    "description": "Evidence-based action guardrails for AI agents. Define versioned policies, collect structured evidence, and verify auditable ALLOW or DENY decisions before tools act.",
    "nav": [
      "How it works",
      "Documentation"
    ],
    "start": "Get started",
    "badge": "OPEN SOURCE",
    "hero": [
      "Let agents act.",
      "With evidence."
    ],
    "intro": "Evidence-based guardrails for your agent’s actions. Keep the policy, checks and decision in one traceable record.",
    "try": "Try it locally",
    "explore": "Explore the docs",
    "meta": [
      "Apache 2.0",
      "Self-hosted",
      "HTTP API + CLI"
    ],
    "heroPanel": "An action, with a record.",
    "evidence": "Structured evidence",
    "decision": "Recorded decision",
    "heroChecks": [
      "Temporary files only",
      "Batch size within limit"
    ],
    "heroFooter": "Policy → Evidence → Decision → Tool verification",
    "flowLabel": "THE EXECUTION CHECKPOINT",
    "flowTitle": "From policy to verified action.",
    "steps": [
      [
        "01",
        "Define",
        "Publish the checks and evidence requirements."
      ],
      [
        "02",
        "Submit",
        "Your agent performs checks and submits evidence."
      ],
      [
        "03",
        "Decide",
        "Read the recorded ALLOW or DENY and its validity."
      ],
      [
        "04",
        "Verify and act",
        "The tool verifies evidence and permissions before acting."
      ]
    ],
    "demoLabel": "SEE THE CHECKPOINT IN ACTION",
    "demoTitle": "Change the request. See the decision.",
    "demoIntro": "Two of these four files are protected. Try selecting only temporary files.",
    "demoNotice": "Browser illustration · no service calls or file operations",
    "request": "Cleanup request",
    "selectFiles": "Select target files",
    "protected": "protected",
    "temporary": "temporary",
    "evidenceToggle": "Evidence available",
    "evaluate": "Evaluate request",
    "narrow": "Select temporary files",
    "assessment": "Assessment",
    "ready": "Awaiting assessment",
    "demoChecks": [
      "Only temporary files",
      "At most 2 files"
    ],
    "reasons": {
      "protected": "The request includes protected business files. Narrow the targets before continuing.",
      "batch": "The request exceeds the two-file batch limit.",
      "allowed": "Both checks pass. In a real integration, the tool verifies the authoritative Run, evidence and current targets before acting.",
      "unknown": "The checks cannot be verified. Missing or uncertain evidence keeps the decision at DENY."
    },
    "demoHint": "Try selecting only the .tmp files, or turn off evidence to see UNKNOWN → DENY.",
    "realDemo": "Run the real cleanup demo",
    "quickTitle": "Start your first guardrail.",
    "quickTabs": [
      "Download",
      "From source",
      "Agent plugin"
    ],
    "copy": "Copy commands",
    "copied": "Copied",
    "copyFailed": "Copy unavailable — select the commands",
    "prereq": "Download the native preview, run from source, or try it in your agent host.",
    "quickHints": [
      "The native service and CLI need no Go, Node.js or Python. The optional demo needs Python 3.9+.",
      "Run these commands with Git, the Go version in go.mod and Python 3.9+. Open the printed Demo URL after choosing a password.",
      "Requires matching native binaries, Python, Node.js (pi 22+ / DeepSeek Harness 24.9+), pnpm for DSH and your host's model login. Full prerequisites are in the guide."
    ],
    "fullGuide": "Follow the complete guide",
    "footer": "Evidence before action.",
    "footerLinks": [
      "Documentation",
      "Contributing",
      "Security",
      "Apache 2.0"
    ],
    "docsTitle": "Documentation",
    "docsIntro": "Run an example, then explore the reference guides as needed.",
    "docsGroups": [
      "Get started",
      "Policy and assessments",
      "Operate and integrate"
    ],
    "onPage": "On this page",
    "edit": "View source on GitHub",
    "back": "Back to documentation",
    "skip": "Skip to content",
    "menu": "Toggle navigation",
    "notFound": "This page is outside the guardrail.",
    "notFoundText": "The address may have moved. Head back to the product page or documentation.",
    "home": "Back to home",
    "platform": "Choose your platform",
    "downloadArchive": "Download",
    "installGuide": "Installation guide",
    "pluginIntro": "Try evidence-driven cleanup in pi or DeepSeek Harness with the same service, CLI and plugin version.",
    "pluginGuide": "Install the Agent plugin",
    "downloadPlugin": "Download plugin bundle",
    "brandHome": "OpenGuardrail home",
    "mainNavigation": "Main navigation",
    "footerNavigation": "Footer navigation",
    "quickNavigation": "Quickstart",
    "docsNavigation": "Documentation",
    "cardPath": "open-guardrail / assessment",
    "cardPreview": "PREVIEW",
    "evidenceFirst": "EVIDENCE BEFORE ACTION",
    "cardScene": "file-cleanup",
    "cardRun": "RUN / 002",
    "targetLimit": "2 targets · limit 2",
    "validityLabel": "validForSeconds",
    "localIllustration": "LOCAL ILLUSTRATION",
    "docsEyebrow": "OPENGUARDRAIL / DOCS",
    "contributors": "contributors",
    "builtOpen": "Built in the open.",
    "releaseDownloads": "View all release downloads",
    "notFoundEyebrow": "404 / PAGE NOT FOUND"
  },
  "zh": {
    "lang": "zh-CN",
    "other": "English",
    "title": "OpenGuardrail — 行动之前，先有证据",
    "description": "面向 AI Agent 的循证操作护栏。通过版本化策略、结构化证据和可审计的 ALLOW / DENY 决策，在工具执行前建立核验点。",
    "nav": [
      "工作原理",
      "文档"
    ],
    "start": "快速开始",
    "badge": "开源",
    "hero": [
      "让 Agent 行动。",
      "让证据先行。"
    ],
    "intro": "为 Agent 的系统操作提供策略、证据和放行决策。工具在行动前核验，过程可追溯。",
    "try": "本地体验",
    "explore": "阅读文档",
    "meta": [
      "Apache 2.0",
      "自主部署",
      "HTTP API + CLI"
    ],
    "heroPanel": "每一次操作，都有一份记录。",
    "evidence": "结构化证据",
    "decision": "持久化决策",
    "heroChecks": [
      "仅操作临时文件",
      "文件数量符合限制"
    ],
    "heroFooter": "策略 → 证据 → 决策 → 工具核验",
    "flowLabel": "执行前的检查点",
    "flowTitle": "从检查到执行，四步走完。",
    "steps": [
      [
        "01",
        "定义策略",
        "发布检查项和证据要求。"
      ],
      [
        "02",
        "提交证据",
        "Agent 执行检查，提交观测结果。"
      ],
      [
        "03",
        "读取决策",
        "获取 ALLOW 或 DENY 及其有效期。"
      ],
      [
        "04",
        "核验执行",
        "工具核验证据和权限，再执行操作。"
      ]
    ],
    "demoLabel": "亲手试试这个检查点",
    "demoTitle": "改一下请求，看看决策。",
    "demoIntro": "四个文件中有两个受保护，试着只选临时文件。",
    "demoNotice": "浏览器交互示意 · 不调用服务，不操作文件",
    "request": "清理请求",
    "selectFiles": "选择目标文件",
    "protected": "受保护",
    "temporary": "临时文件",
    "evidenceToggle": "能够提供证据",
    "evaluate": "评估请求",
    "narrow": "仅选择临时文件",
    "assessment": "评估结果",
    "ready": "等待评估",
    "demoChecks": [
      "仅包含临时文件",
      "最多 2 个文件"
    ],
    "reasons": {
      "protected": "请求包含受保护的业务文件，需要缩小目标范围后再继续。",
      "batch": "请求超过了每批最多两个文件的限制。",
      "allowed": "检查通过。真实集成中，工具仍需核验权威 Run、证据和当前目标后执行。",
      "unknown": "无法核验检查结果。证据缺失或情况不确定时，决策保持 DENY。"
    },
    "demoHint": "试试仅选择 .tmp 文件，或关闭证据，观察 UNKNOWN → DENY。",
    "realDemo": "运行真实的清理 Demo",
    "quickTitle": "开始你的第一次护栏体验。",
    "quickTabs": [
      "下载运行",
      "源码运行",
      "Agent 插件"
    ],
    "copy": "复制命令",
    "copied": "已复制",
    "copyFailed": "无法自动复制，请选择命令文本",
    "prereq": "下载原生包、从源码运行，或在 Agent 宿主中试用。",
    "quickHints": [
      "原生服务与 CLI 无需 Go、Node.js 或 Python；可选 Demo 需要 Python 3.9+。",
      "需要 Git、go.mod 指定的 Go 和 Python 3.9+。设置密码后，打开终端输出的 Demo URL。",
      "需配套原生二进制、Python、Node.js（pi 22+ / DeepSeek Harness 24.9+）、DSH 所需的 pnpm 和宿主模型登录。完整前置条件见指南。"
    ],
    "fullGuide": "阅读完整指南",
    "footer": "行动之前，先有证据。",
    "footerLinks": [
      "文档",
      "参与贡献",
      "安全策略",
      "Apache 2.0"
    ],
    "docsTitle": "文档",
    "docsIntro": "先运行示例，需要时再查 API 和部署指南。",
    "docsGroups": [
      "开始使用",
      "策略与评估",
      "部署与集成"
    ],
    "onPage": "本页目录",
    "edit": "在 GitHub 查看源码",
    "back": "返回文档",
    "skip": "跳转到正文",
    "menu": "展开导航",
    "notFound": "这个页面不在护栏范围内。",
    "notFoundText": "地址可能已经变化，请返回产品首页或文档。",
    "home": "返回首页",
    "platform": "选择运行平台",
    "downloadArchive": "下载",
    "installGuide": "安装与校验",
    "pluginIntro": "在 pi 或 DeepSeek Harness 中体验循证清理，使用同一版本的服务、CLI 和插件。",
    "pluginGuide": "安装 Agent 插件",
    "downloadPlugin": "下载插件安装包",
    "brandHome": "OpenGuardrail 首页",
    "mainNavigation": "主导航",
    "footerNavigation": "页脚导航",
    "quickNavigation": "快速开始",
    "docsNavigation": "文档导航",
    "cardPath": "open-guardrail / 评估",
    "cardPreview": "预览",
    "evidenceFirst": "行动之前，先有证据",
    "cardScene": "文件清理",
    "cardRun": "运行 / 002",
    "targetLimit": "2 个目标 · 上限 2 个",
    "validityLabel": "有效期（秒）",
    "localIllustration": "本地交互示意",
    "docsEyebrow": "OPENGUARDRAIL / 文档",
    "contributors": "贡献者",
    "builtOpen": "开放共建。",
    "releaseDownloads": "查看全部发行下载",
    "notFoundEyebrow": "404 / 页面未找到"
  }
};
