export const copy = {
  "en": {
    "lang": "en",
    "other": "简体中文",
    "title": "OpenGuardrail — Evidence before action",
    "description": "Don't rely on system prompts to stop risky AI agent actions. OpenGuardrail enforces evidence-based ALLOW / DENY gates before destructive MCP tool calls and production rollouts.",
    "nav": [
      "How it works",
      "Documentation"
    ],
    "start": "Get started",
    "badge": "OPEN SOURCE",
    "hero": [
      "Evidence-based guardrails",
      "for AI agents"
    ],
    "intro": "Don't trust system prompts alone to stop risky agent actions. Require verified evidence and auditable ALLOW decisions before destructive MCP tool calls and production rollouts.",
    "try": "Run the first-batch check",
    "explore": "Explore the docs",
    "meta": [
      "Apache 2.0",
      "MCP Proxy + GitHub Actions",
      "Self-hosted Go"
    ],
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
        "Read decision",
        "Read the recorded ALLOW or DENY and its validity."
      ],
      [
        "04",
        "Verify and act",
        "The tool verifies evidence and permissions before acting."
      ]
    ],
    "demoLabel": "OPTIONAL FILE CLEANUP",
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
    "quickTitle": "Try OpenGuardrail locally",
    "quickTabs": [
      "First batch · Source",
      "Native binaries · Download",
      "Cleanup · Plugin"
    ],
    "copy": "Copy commands",
    "copied": "Copied",
    "copyFailed": "Copy unavailable — select the commands",
    "prereq": "For the source walkthrough, use Git, Make, Python 3.9+, the Go version in go.mod and a coding agent with shell access (Claude Code, Cursor, Codex, etc.).",
    "quickHints": [
      "First-batch rollout scenario: open the checkout in your coding agent and ask: Use $open-guardrail-demo to check the first batch and tell me whether the next batch can proceed.",
      "Prebuilt native binaries (no Go, Node.js or Python needed for the service and CLI; Python 3.9+ for the optional cleanup demo). For guardrailctl mcp-proxy before the next tagged release, install from main with go install github.com/alibaba/open-guardrail/cmd/guardrailctl@main.",
      "File-cleanup scenario in pi / DSH (PreToolUse tool adapter): requires matching native binaries, Python, Node.js (22+ for pi / 24.9+ for DeepSeek Harness), pnpm for DSH and your host's model login. Full prerequisites are in the guide."
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
    "cardScene": "file-cleanup",
    "localIllustration": "LOCAL ILLUSTRATION",
    "docsEyebrow": "OPENGUARDRAIL / DOCS",
    "contributors": "contributors",
    "builtOpen": "Built in the open.",
    "releaseDownloads": "View all release downloads",
    "notFoundEyebrow": "404 / PAGE NOT FOUND",
    "rollout": {
      "label": "Code rollout · example pre-resume check",
      "video": "Watch the 30-second run",
      "title": "The first batch is live. Can the next batch proceed?",
      "context": "After the first batch deploys, the on-call engineer checks metrics, logs and alerts before deciding whether to roll out the second batch. At Alibaba, we use OpenGuardrail for this check in production. This local example follows the Spec in a simulated checkout service rollout to investigate a failed write affecting a synthetic order.",
      "columns": [
        "Evidence source",
        "What the agent finds"
      ],
      "outcome": "The write failures affected business data (verdict: BLOCKED). After the agent submits its findings and evidence, OpenGuardrail records DENY so the rollout stops before the next batch.",
      "link": "How to inspect a Run report",
      "rows": [
        [
          "Metrics and alerts",
          "The agent checks the error rate and active alerts for the first batch. The error rate is within the threshold and there are no active alerts."
        ],
        [
          "Error logs",
          "New write errors appear on those same instances after deployment."
        ],
        [
          "Code and data",
          "The diff touches the failing write path. A read-only lookup shows payment for order-42 was captured while the order is still pending."
        ]
      ]
    }
  },
  "zh": {
    "lang": "zh-CN",
    "other": "English",
    "title": "OpenGuardrail — 行动之前，先有证据",
    "description": "别只靠提示词约束 AI Agent：在高危 MCP 工具调用、删库操作和生产发布前，强制要求结构化证据与可审计的 ALLOW / DENY 放行决策。",
    "nav": [
      "工作原理",
      "文档"
    ],
    "start": "快速开始",
    "badge": "开源",
    "hero": [
      "面向 AI Agent 的",
      "循证操作护栏"
    ],
    "intro": "别只靠系统提示词祈祷 AI Agent 不出事故——在删库、高危 MCP 调用和生产发布前，强制要求结构化证据与可审计放行。",
    "try": "运行首批发布检查",
    "explore": "阅读文档",
    "meta": [
      "Apache 2.0",
      "MCP Proxy + GitHub Actions",
      "原生二进制私有化部署"
    ],
    "flowLabel": "执行前的检查点",
    "flowTitle": "从定义策略到核验执行。",
    "steps": [
      [
        "01",
        "定义策略",
        "配置检查项和证据要求。"
      ],
      [
        "02",
        "提交证据",
        "Agent 执行检查并提交证据。"
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
    "demoLabel": "在线交互演示 · 文件清理",
    "demoTitle": "调整请求，实时看决策结果",
    "demoIntro": "四个文件中有两个受保护，试着只勾选临时文件。",
    "demoNotice": "浏览器前端演示 · 不发起服务请求，不操作真实文件",
    "request": "清理请求",
    "selectFiles": "选择目标文件",
    "protected": "受保护",
    "temporary": "临时文件",
    "evidenceToggle": "提供完整证据",
    "evaluate": "评估请求",
    "narrow": "仅保留临时文件",
    "assessment": "评估结果",
    "ready": "等待评估",
    "demoChecks": [
      "仅限临时文件",
      "单批最多 2 个文件"
    ],
    "reasons": {
      "protected": "请求中包含受保护的业务文件，请先缩小目标范围。",
      "batch": "请求数量超过了单批最多 2 个文件的限制。",
      "allowed": "两项检查均已通过。真实接入时，工具仍会核验权威 Run、证据及当前目标状态后才执行。",
      "unknown": "缺少可核验的检查证据。证据缺失或状态存疑时，默认拦截为 DENY。"
    },
    "demoHint": "试试只勾选 .tmp 文件，或关闭证据开关，观察 UNKNOWN → DENY 的兜底拦截。",
    "realDemo": "运行真实文件清理 Demo",
    "quickTitle": "在本地体验 OpenGuardrail",
    "quickTabs": [
      "首批检查 · 源码",
      "原生二进制 · 下载",
      "文件清理 · 插件"
    ],
    "copy": "复制命令",
    "copied": "已复制",
    "copyFailed": "无法自动复制，请选择命令文本",
    "prereq": "源码体验需要 Git、Make、Python 3.9+、go.mod 指定的 Go 版本，以及支持执行命令的 AI 编程助手（如 Claude Code、Cursor、Codex 等）。",
    "quickHints": [
      "首批发布复核场景：在你的 AI 编程助手中打开仓库，发送：“用 $open-guardrail-demo 跑一遍首批检查，告诉我下一批能不能发。”",
      "预构建原生二进制（运行服务与 CLI 无需 Go / Node.js / Python；可选文件清理示例需 Python 3.9+）。在新版 Release 发布前，体验 guardrailctl mcp-proxy 可通过 go install github.com/alibaba/open-guardrail/cmd/guardrailctl@main 从 main 安装。",
      "pi / DSH 文件清理场景（PreToolUse 工具拦截适配器）：需配套原生二进制、Python、Node.js（pi 需 22+ / DeepSeek Harness 需 24.9+）、DSH 所需的 pnpm，以及在宿主工具中完成模型认证。完整前置条件见指南。"
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
    "cardScene": "文件清理",
    "localIllustration": "本地交互示意",
    "docsEyebrow": "OPENGUARDRAIL / 文档",
    "contributors": "贡献者",
    "builtOpen": "开放共建。",
    "releaseDownloads": "查看全部发行下载",
    "notFoundEyebrow": "404 / 页面未找到",
    "rollout": {
      "label": "代码发布 · 放行前复核示例",
      "video": "观看 30 秒检查回放",
      "title": "首批发布完成后，下一批还能继续吗？",
      "context": "第一批机器上线后，值班同学通常要看监控、查日志、核对告警，确认没问题再放行第二批——在阿里巴巴内部，我们已经将 OpenGuardrail 落地在这一步。下面以模拟的结算服务发布为例，演示 Agent 如何按 Spec 追查一次写库失败。",
      "columns": [
        "证据来源",
        "Agent 排查结果"
      ],
      "outcome": "写库异常已波及业务订单（检查结论为 BLOCKED），Agent 上报检查结果与证据后，OpenGuardrail 记录 DENY，在放行下一批之前及时拦截风险。",
      "link": "查看 Run 排障报告说明",
      "rows": [
        [
          "监控与告警",
          "核对首批实例的错误率与活跃告警：错误率处于阈值内，无活跃告警。"
        ],
        [
          "错误日志",
          "同一批实例在发布后出现了新的写库异常。"
        ],
        [
          "代码与数据",
          "本次改动恰好涉及报错的写入路径；只读查询确认，订单 order-42 已扣款但状态仍停留在 pending。"
        ]
      ]
    }
  }
};
