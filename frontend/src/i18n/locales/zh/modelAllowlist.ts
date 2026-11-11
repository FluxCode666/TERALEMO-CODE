export default {
  modelAllowlist: {
    title: '模型请求 IP / 域名白名单', hint: '先保存客户出口和备注，观察访问来源，确认后单独开启拦截。网页控制台不受影响。',
    enable: '开启拦截', disable: '关闭拦截', entry: 'IPv4 / IPv6 / CIDR / 域名', note: '客户名称 / 备注', add: '新增条目', save: '仅保存条目和代理设置',
    cloudflare: '信任 Cloudflare 官方 CDN 网段', cdn: '其他可信 CDN IP / CIDR', cdnHint: '每行一个，必须是 CDN 服务商确认的回源地址',
    proxyHint: '本机 nginx / Docker 对端由 server.trusted_proxies 配置。代理必须覆盖客户端 IP 头或追加真实对端，未受信任来源的转发头不会被采用。',
    preview: '校验并预览 DNS', testIP: '输入客户出口 IP', test: '测试白名单', allowed: '该 IP 命中白名单', denied: '开启后该 IP 将被拦截', testHint: '测试按开启状态计算全局白名单；每个 Key 自身的 IP 限制仍独立生效。',
    observations: '观察：未命中白名单的真实出口', observationHint: '按 IP、用户和 Key 聚合，保留 7 天，最多 10,000 组。Redis 不可用时观察数据可能缺失，请核对流量后再开启。',
    user: '用户', key: 'Key 名称 / ID', count: '次数', lastSeen: '最近请求', addObserved: '加入并保存', refresh: '刷新观察数据', empty: '暂无未命中来源',
    confirm: '按已保存的白名单，最近 24 小时有 {ips} 个 IP、{users} 个用户、{keys} 个 Key 的请求会被拦截。开启后必须同时通过全局和 Key 自身的白名单。确认开启？', failed: '操作失败，请重试',
  },
}
