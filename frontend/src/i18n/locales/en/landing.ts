export default {
  batchImageGuide: {
    title: 'Batch Image Generation',
    description: 'Submit multiple prompts in one job and download the generated images when complete'
  },
  // Home Page
  home: {
    viewDocs: 'View Documentation',
    docs: 'Docs',
    switchToLight: 'Switch to Light Mode',
    switchToDark: 'Switch to Dark Mode',
    dashboard: 'Dashboard',
    login: 'Login',
    getStarted: 'Get Started',
    goToDashboard: 'Go to Dashboard',
    skipToContent: 'Skip to content',
    nav: {
      label: 'Home navigation',
      access: 'Access',
      governance: 'Governance',
      security: 'Audit',
      onboarding: 'Onboarding'
    },
    hero: {
      eyebrow: 'Enterprise AI access layer',
      titleLead: 'Bring AI models',
      titleAccent: 'into every layer of your org',
      description:
        '{name} gives enterprises one gateway to leading chat, image and video models — with organization-wide quota governance and end-to-end audit. Allocate, meter and trace usage by department and member.',
      primaryGuest: 'Enterprise sign in',
      primaryAuthed: 'Open console',
      secondaryDocs: 'Read the docs',
      secondaryPlaza: 'Browse models',
      secondaryOnboarding: 'How onboarding works',
      note: 'Admins can bulk-import member accounts — no one-by-one sign-ups'
    },
    map: {
      label: 'Illustration: the organization drawn as contour lines, one ring per department level',
      enterprise: 'Enterprise',
      rd: 'R&D Center',
      rdMeta: '62% of monthly quota',
      backend: 'Backend Team',
      backendMeta: 'Near limit · 85%',
      requestMeta: '200 · 1.24s',
      legend: 'Each contour is one department level — up to 8',
      rollup: 'Usage rolls up to parent levels'
    },
    facts: {
      label: 'Platform at a glance',
      levels: { unit: 'levels', label: 'Department depth', desc: 'Model your real reporting lines' },
      periods: { unit: 'periods', label: 'Quota windows', desc: 'Daily / weekly / monthly / lifetime caps' },
      protocols: { unit: 'protocols', label: 'Native compatibility', desc: 'OpenAI / Anthropic / Gemini' },
      key: { unit: 'key', label: 'For every granted model', desc: 'Granted by group, metered per call' }
    },
    access: {
      kicker: 'Unified access',
      title: 'One gateway to the models that matter',
      description:
        'Natively compatible with OpenAI, Anthropic and Gemini protocols — existing SDKs and tools only need a new base URL and key. Chat, image and video share one auth and one meter.',
      points: {
        protocol: {
          title: 'Native protocols, zero rewrites',
          desc: 'Business code and tools like Claude Code or Codex work as-is — just point them here.'
        },
        routing: {
          title: 'Multi-upstream scheduling',
          desc: 'Upstream account pools with load balancing, failover and sticky sessions for long tasks.'
        },
        media: {
          title: 'Beyond chat: image and video',
          desc: 'Image and video generation share the same keys and bill, with a ready-to-use web studio.'
        }
      },
      panel: {
        title: 'Connection details',
        baseUrl: 'Base URL',
        copy: 'Copy',
        copied: 'Copied',
        protocol: 'Protocol',
        endpoint: 'Endpoint',
        keyNote: 'Create an API key in the console and bind it to a group to call every model that group allows.'
      }
    },
    governance: {
      kicker: 'Governance',
      title: 'Quotas and permissions that follow your org chart',
      description:
        'Build a hierarchy of departments and members; quotas and callable models narrow level by level. Who is using what, how much, and what is left — in one table.',
      items: {
        org: {
          title: 'Org structure',
          desc: 'Up to 8 department levels; bulk-import members via XLSX / CSV, missing paths are created for you.'
        },
        quota: {
          title: 'Four quota windows',
          desc: 'Set daily / weekly / monthly / lifetime caps for the enterprise, departments and members — 0 blocks calls.'
        },
        groups: {
          title: 'Group grants',
          desc: 'Child departments can only narrow the model groups their parent allows.'
        },
        roles: {
          title: 'Clear roles',
          desc: 'Owner, department manager and member roles, each with its own permissions and visibility.'
        }
      },
      table: {
        title: 'Quotas · this month',
        department: 'Department',
        usage: 'Used / monthly cap',
        unlimited: 'Unlimited',
        rows: {
          rd: 'R&D Center',
          backend: 'Backend Team',
          algorithm: 'Algorithms',
          marketing: 'Marketing',
          design: 'Design'
        },
        status: {
          normal: 'Normal',
          warning: 'Near limit',
          unlimited: 'Unlimited',
          disabled: 'Disabled'
        },
        footnote: 'Parent usage includes all child departments'
      }
    },
    security: {
      kicker: 'Audit & security',
      title: 'Every call leaves a trail',
      description:
        'Request logs, admin audit and content audit cover the full path — built for internal controls and compliance retention.',
      items: {
        usage: { title: 'Usage analytics', desc: 'Break usage down by department, member and model; export to CSV.' },
        audit: { title: 'Admin audit', desc: 'Member changes, quota edits and permission updates are all recorded.' },
        prompt: { title: 'Content audit', desc: 'Audit and retain prompts so sensitive requests stay traceable.' },
        risk: { title: 'Risk control', desc: 'Detect abnormal call patterns and stop them early.' },
        sso: { title: 'Single sign-on', desc: 'Sign in with DingTalk, OIDC and other enterprise identity providers.' },
        notify: { title: 'Balance alerts', desc: 'Notify the right owners when balance or quota runs low.' }
      },
      ledger: {
        title: 'Audit log',
        live: 'Live',
        rows: {
          call: 'Backend · Z***',
          quota: 'Admin · changed Marketing monthly cap',
          image: 'Algorithms · L***',
          blocked: 'System · blocked a Design member call',
          blockedMeta: 'Disabled'
        }
      }
    },
    onboarding: {
      kicker: 'Onboarding',
      title: 'Four steps to go live',
      steps: {
        open: { title: 'Open an enterprise space', desc: 'Confirm your needs with us, then we open the account and assign an owner.' },
        org: { title: 'Build the organization', desc: 'Import members and departments, and appoint department managers.' },
        quota: { title: 'Set quotas and models', desc: 'Configure caps and callable groups for the enterprise, departments and members.' },
        key: { title: 'Create keys, start calling', desc: 'Members create keys in the console and plug them into existing tools.' }
      }
    },
    cta: {
      title: 'Make AI part of your infrastructure',
      description: 'Already have an enterprise account? Sign in to the console. To open an account or evaluate access, talk to our team.',
      contact: 'Business contact'
    },
    footer: {
      tagline: 'Enterprise AI access & governance',
      product: 'Product',
      account: 'Account',
      keyUsage: 'Key usage lookup',
      allRightsReserved: 'All rights reserved.'
    }
  },

  // Key Usage Query Page
  keyUsage: {
    title: 'API Key Usage',
    subtitle: 'Enter your API Key to view real-time spending and usage status',
    placeholder: 'sk-ant-mirror-xxxxxxxxxxxx',
    query: 'Query',
    querying: 'Querying...',
    privacyNote: 'Your Key is processed locally in the browser and will not be stored',
    dateRange: 'Date Range:',
    dateRangeToday: 'Today',
    dateRange7d: '7 Days',
    dateRange30d: '30 Days',
    dateRange90d: '90 Days',
    dateRangeCustom: 'Custom',
    apply: 'Apply',
    used: 'Used',
    detailInfo: 'Detail Information',
    tokenStats: 'Token Statistics',
    dailyDetail: 'Daily Detail',
    modelStats: 'Model Usage Statistics',
    // Table headers
    date: 'Date',
    model: 'Model',
    requests: 'Requests',
    inputTokens: 'Input Tokens',
    outputTokens: 'Output Tokens',
    cacheCreationTokens: 'Cache Creation',
    cacheReadTokens: 'Cache Read',
    cacheWriteTokens: 'Cache Write',
    totalTokens: 'Total Tokens',
    cost: 'Cost',
    // Status
    quotaMode: 'Key Quota Mode',
    walletBalance: 'Wallet Balance',
    // Ring card titles
    totalQuota: 'Total Quota',
    limit5h: '5-Hour Limit',
    limitDaily: 'Daily Limit',
    limit7d: '7-Day Limit',
    limitWeekly: 'Weekly Limit',
    limitMonthly: 'Monthly Limit',
    // Detail rows
    remainingQuota: 'Remaining Quota',
    expiresAt: 'Expires At',
    todayExpires: '(expires today)',
    daysLeft: '({days} days)',
    usedQuota: 'Used Quota',
    resetNow: 'Resetting soon',
    subscriptionType: 'Subscription Type',
    billingType: 'Billing Type',
    subscriptionExpires: 'Subscription Expires',
    // Usage stat cells
    todayRequests: 'Today Requests',
    todayInputTokens: 'Today Input',
    todayOutputTokens: 'Today Output',
    todayTokens: 'Today Tokens',
    todayCacheCreation: 'Today Cache Creation',
    todayCacheRead: 'Today Cache Read',
    todayCost: 'Today Cost',
    rpmTpm: 'RPM / TPM',
    totalRequests: 'Total Requests',
    totalInputTokens: 'Total Input',
    totalOutputTokens: 'Total Output',
    totalTokensLabel: 'Total Tokens',
    totalCacheCreation: 'Total Cache Creation',
    totalCacheRead: 'Total Cache Read',
    totalCost: 'Total Cost',
    avgDuration: 'Avg Duration',
    // Messages
    enterApiKey: 'Please enter an API Key',
    querySuccess: 'Query successful',
    queryFailed: 'Query failed',
    queryFailedRetry: 'Query failed, please try again later',
    noDailyUsage: 'No daily usage data',
  },

  // Setup Wizard
  setup: {
    title: 'Sub2API Setup',
    description: 'Configure your Sub2API instance',
    database: {
      title: 'Database Configuration',
      description: 'Connect to your PostgreSQL database',
      host: 'Host',
      port: 'Port',
      username: 'Username',
      password: 'Password',
      databaseName: 'Database Name',
      sslMode: 'SSL Mode',
      passwordPlaceholder: 'Password',
      ssl: {
        disable: 'Disable',
        require: 'Require',
        verifyCa: 'Verify CA',
        verifyFull: 'Verify Full'
      }
    },
    redis: {
      title: 'Redis Configuration',
      description: 'Connect to your Redis server',
      host: 'Host',
      port: 'Port',
      username: 'Username (optional)',
      password: 'Password (optional)',
      database: 'Database',
      usernamePlaceholder: 'Leave empty for default user',
      passwordPlaceholder: 'Password',
      enableTls: 'Enable TLS',
      enableTlsHint: 'Use TLS when connecting to Redis (public CA certs)'
    },
    admin: {
      title: 'Admin Account',
      description: 'Create your administrator account',
      email: 'Email',
      password: 'Password',
      confirmPassword: 'Confirm Password',
      passwordPlaceholder: 'Min 8 characters',
      confirmPasswordPlaceholder: 'Confirm password',
      passwordMismatch: 'Passwords do not match'
    },
    ready: {
      title: 'Ready to Install',
      description: 'Review your configuration and complete setup',
      database: 'Database',
      redis: 'Redis',
      adminEmail: 'Admin Email'
    },
    status: {
      testing: 'Testing...',
      success: 'Connection Successful',
      testConnection: 'Test Connection',
      installing: 'Installing...',
      completeInstallation: 'Complete Installation',
      completed: 'Installation completed!',
      redirecting: 'Redirecting to login page...',
      restarting: 'Service is restarting, please wait...',
      timeout: 'Service restart is taking longer than expected. Please refresh the page manually.'
    }
  },

  // Common
}
