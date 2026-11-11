export default {
  batchImageGuide: {
    title: '图片批量生成',
    description: '一次提交多条提示词，任务完成后可统一下载图片结果'
  },
  // Home Page
  home: {
    viewDocs: '查看文档',
    docs: '文档',
    switchToLight: '切换到浅色模式',
    switchToDark: '切换到深色模式',
    dashboard: '控制台',
    login: '登录',
    getStarted: '立即开始',
    goToDashboard: '进入控制台',
    skipToContent: '跳到正文',
    nav: {
      label: '首页导航',
      access: '统一接入',
      governance: '组织治理',
      security: '安全审计',
      onboarding: '接入流程'
    },
    hero: {
      eyebrow: '企业级 AI 接入层',
      titleLead: '把大模型',
      titleAccent: '接入组织的每一层',
      description:
        '{name} 为企业提供统一的模型接入、组织级额度治理与全链路审计。一个入口对接主流对话、图像与视频模型，按部门与成员分配、计量、追溯。',
      primaryGuest: '企业登录',
      primaryAuthed: '进入控制台',
      secondaryDocs: '查看接入文档',
      secondaryPlaza: '浏览模型广场',
      secondaryOnboarding: '了解接入流程',
      note: '成员账号可由企业管理员批量导入，无需逐个注册'
    },
    map: {
      label: '示意图：企业组织以等高线表示，每一圈代表一级部门',
      enterprise: '企业',
      rd: '研发中心',
      rdMeta: '月限额已用 62%',
      backend: '后端组',
      backendMeta: '接近上限 · 85%',
      requestMeta: '200 · 1.24s',
      legend: '每一圈等高线代表一级部门，最多 8 级',
      rollup: '用量逐级汇总至上级'
    },
    facts: {
      label: '平台关键能力',
      levels: { unit: '级', label: '部门层级', desc: '组织架构按真实汇报关系建模' },
      periods: { unit: '档', label: '限额周期', desc: '日 / 周 / 月 / 累计，分别设上限' },
      protocols: { unit: '种', label: '原生协议兼容', desc: 'OpenAI / Anthropic / Gemini' },
      key: { unit: '个', label: 'Key 调用授权模型', desc: '按分组授权，按调用计量' }
    },
    access: {
      kicker: '统一接入',
      title: '一个入口，接入主流模型',
      description:
        '兼容 OpenAI、Anthropic 与 Gemini 原生协议，现有 SDK 与开发工具只需替换 Base URL 和 Key。对话、图像、视频统一鉴权、统一计量。',
      points: {
        protocol: {
          title: '原生协议，零改造',
          desc: '业务代码与 Claude Code、Codex 等开发工具无需改写，替换地址即可接入。'
        },
        routing: {
          title: '多上游调度',
          desc: '上游账号池自动负载均衡与故障切换，会话保持避免长任务上下文中断。'
        },
        media: {
          title: '对话之外，图像与视频',
          desc: '生图、视频生成与对话共用一套 Key 与账单，网页端创作台开箱即用。'
        }
      },
      panel: {
        title: '接入参数',
        baseUrl: 'Base URL',
        copy: '复制',
        copied: '已复制',
        protocol: '协议',
        endpoint: '端点',
        keyNote: 'API Key 在控制台创建，绑定分组后即可调用该分组授权的全部模型。'
      }
    },
    governance: {
      kicker: '组织治理',
      title: '额度与权限，跟着组织架构走',
      description:
        '按部门与成员建立层级，额度与可调用模型逐级收窄。谁在用、用了多少、还剩多少，一张表看清。',
      items: {
        org: {
          title: '组织架构',
          desc: '最多 8 级部门；支持 XLSX / CSV 批量导入成员，不存在的部门路径自动创建。'
        },
        quota: {
          title: '四档限额',
          desc: '企业、部门、成员均可设置日 / 周 / 月 / 累计上限，设为 0 即禁止调用。'
        },
        groups: {
          title: '分组授权',
          desc: '下级部门只能在上级允许的范围内，进一步收窄可调用的模型分组。'
        },
        roles: {
          title: '角色分工',
          desc: '企业负责人、部门负责人、员工三级角色，权限与可见范围随之变化。'
        }
      },
      table: {
        title: '限额管理 · 本月',
        department: '部门',
        usage: '已用 / 月限额',
        unlimited: '不限',
        rows: {
          rd: '研发中心',
          backend: '后端组',
          algorithm: '算法组',
          marketing: '市场部',
          design: '设计部'
        },
        status: {
          normal: '正常',
          warning: '接近上限',
          unlimited: '不限',
          disabled: '已禁用'
        },
        footnote: '上级部门的用量包含全部下级部门'
      }
    },
    security: {
      kicker: '安全审计',
      title: '每一次调用，都有据可查',
      description: '调用日志、操作审计与内容审计贯穿全链路，满足企业内控与合规留档需要。',
      items: {
        usage: { title: '用量分析', desc: '按部门、成员、模型拆分用量，支持导出 CSV。' },
        audit: { title: '操作审计', desc: '成员变更、限额调整、权限修改全部留痕。' },
        prompt: { title: '内容审计', desc: '对提示词进行审计与留档，敏感请求可追溯。' },
        risk: { title: '风险控制', desc: '识别异常调用行为，及时拦截与处置。' },
        sso: { title: '统一登录', desc: '支持钉钉、OIDC 等企业身份源登录。' },
        notify: { title: '余额提醒', desc: '余额或额度不足时及时通知相关负责人。' }
      },
      ledger: {
        title: '审计日志',
        live: '实时',
        rows: {
          call: '后端组 · 张**',
          quota: '管理员 · 调整「市场部」月限额',
          image: '算法组 · 李**',
          blocked: '系统 · 拦截「设计部」成员调用',
          blockedMeta: '已禁用'
        }
      }
    },
    onboarding: {
      kicker: '接入流程',
      title: '四步完成企业接入',
      steps: {
        open: { title: '开通企业空间', desc: '与我们确认需求，开通企业账户并指定企业负责人。' },
        org: { title: '搭建组织', desc: '导入成员与部门，指派各部门负责人。' },
        quota: { title: '配置额度与模型', desc: '为企业、部门与成员设置限额和可调用分组。' },
        key: { title: '创建 Key，开始调用', desc: '成员在控制台创建 Key，接入现有工具与业务系统。' }
      }
    },
    cta: {
      title: '让 AI 成为组织的基础设施',
      description: '已开通企业账户，直接登录控制台；需要开通或评估接入，欢迎联系商务对接。',
      contact: '商务对接'
    },
    footer: {
      tagline: '企业级 AI 接入与治理平台',
      product: '产品',
      account: '账户',
      keyUsage: 'Key 用量查询',
      allRightsReserved: '保留所有权利。'
    }
  },

  // Key Usage Query Page
  keyUsage: {
    title: 'API Key 用量查询',
    subtitle: '输入您的 API Key 以查看实时消费金额与使用状态',
    placeholder: 'sk-ant-mirror-xxxxxxxxxxxx',
    query: '查询',
    querying: '查询中...',
    privacyNote: '您的 Key 仅在浏览器本地处理，不会被存储',
    dateRange: '统计范围:',
    dateRangeToday: '今日',
    dateRange7d: '7 天',
    dateRange30d: '30 天',
    dateRange90d: '90 天',
    dateRangeCustom: '自定义',
    apply: '应用',
    used: '已使用',
    detailInfo: '详细信息',
    tokenStats: 'Token 统计',
    dailyDetail: '按日明细',
    modelStats: '模型用量统计',
    // Table headers
    date: '日期',
    model: '模型',
    requests: '请求数',
    inputTokens: '输入 Tokens',
    outputTokens: '输出 Tokens',
    cacheCreationTokens: '缓存创建',
    cacheReadTokens: '缓存读取',
    cacheWriteTokens: '缓存写入',
    totalTokens: '总 Tokens',
    cost: '费用',
    // Status
    quotaMode: 'Key 限额模式',
    walletBalance: '钱包余额',
    // Ring card titles
    totalQuota: '总额度',
    limit5h: '5 小时限额',
    limitDaily: '日限额',
    limit7d: '7 天限额',
    limitWeekly: '周限额',
    limitMonthly: '月限额',
    // Detail rows
    remainingQuota: '剩余额度',
    expiresAt: '过期时间',
    todayExpires: '(今日到期)',
    daysLeft: '({days} 天)',
    usedQuota: '已用额度',
    resetNow: '即将重置',
    subscriptionType: '订阅类型',
    billingType: '计费方式',
    subscriptionExpires: '订阅到期',
    // Usage stat cells
    todayRequests: '今日请求',
    todayInputTokens: '今日输入',
    todayOutputTokens: '今日输出',
    todayTokens: '今日 Tokens',
    todayCacheCreation: '今日缓存创建',
    todayCacheRead: '今日缓存读取',
    todayCost: '今日费用',
    rpmTpm: 'RPM / TPM',
    totalRequests: '累计请求',
    totalInputTokens: '累计输入',
    totalOutputTokens: '累计输出',
    totalTokensLabel: '累计 Tokens',
    totalCacheCreation: '累计缓存创建',
    totalCacheRead: '累计缓存读取',
    totalCost: '累计费用',
    avgDuration: '平均耗时',
    // Messages
    enterApiKey: '请输入 API Key',
    querySuccess: '查询成功',
    queryFailed: '查询失败',
    queryFailedRetry: '查询失败，请稍后重试',
    noDailyUsage: '暂无按日用量数据',
  },

  // Setup Wizard
  setup: {
    title: 'Sub2API 安装向导',
    description: '配置您的 Sub2API 实例',
    database: {
      title: '数据库配置',
      description: '连接到您的 PostgreSQL 数据库',
      host: '主机',
      port: '端口',
      username: '用户名',
      password: '密码',
      databaseName: '数据库名称',
      sslMode: 'SSL 模式',
      passwordPlaceholder: '密码',
      ssl: {
        disable: '禁用',
        require: '要求',
        verifyCa: '验证 CA',
        verifyFull: '完全验证'
      }
    },
    redis: {
      title: 'Redis 配置',
      description: '连接到您的 Redis 服务器',
      host: '主机',
      port: '端口',
      username: '用户名（可选）',
      password: '密码（可选）',
      database: '数据库',
      usernamePlaceholder: '默认用户留空',
      passwordPlaceholder: '密码',
      enableTls: '启用 TLS',
      enableTlsHint: '连接 Redis 时使用 TLS（公共 CA 证书）'
    },
    admin: {
      title: '管理员账户',
      description: '创建您的管理员账户',
      email: '邮箱',
      password: '密码',
      confirmPassword: '确认密码',
      passwordPlaceholder: '至少 8 个字符',
      confirmPasswordPlaceholder: '确认密码',
      passwordMismatch: '密码不匹配'
    },
    ready: {
      title: '准备安装',
      description: '检查您的配置并完成安装',
      database: '数据库',
      redis: 'Redis',
      adminEmail: '管理员邮箱'
    },
    status: {
      testing: '测试中...',
      success: '连接成功',
      testConnection: '测试连接',
      installing: '安装中...',
      completeInstallation: '完成安装',
      completed: '安装完成！',
      redirecting: '正在跳转到登录页面...',
      restarting: '服务正在重启，请稍候...',
      timeout: '服务重启时间超出预期，请手动刷新页面。'
    }
  },

  // Common
}
