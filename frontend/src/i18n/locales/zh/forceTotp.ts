export default {
  forceTotp: {
    title: '强制所有账号配置 TOTP 双因素认证', hint: '包括管理员。未配置账号的现有会话会被撤销，重新登录后只可配置 TOTP。Passkey 不替代 TOTP；API Key 模型请求不受此开关影响。',
    keyRequired: '请先在部署配置中固定 TOTP 加密密钥，再开启此功能。', enable: '开启强制 2FA', disable: '关闭强制 2FA',
    confirm: '当前有 {count} 个账号未配置 TOTP。开启后这些账号将立即退出所有会话和设备；重新登录后必须完成 TOTP 配置才能继续。确认开启？',
    setupTitle: '请先配置双因素认证', setupHint: '管理员要求所有账号使用 TOTP。完成配置后即可继续使用控制台。', setup: '配置 TOTP', logout: '退出登录', failed: '操作失败，请重试',
  },
}
