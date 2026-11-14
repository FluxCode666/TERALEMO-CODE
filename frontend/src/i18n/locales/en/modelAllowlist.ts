export default {
  modelAllowlist: {
    title: 'Model request IP / domain allowlist', hint: 'Save customer egress addresses and notes first, observe traffic, then enable enforcement separately. The web console is unaffected.',
    enable: 'Enable enforcement', disable: 'Disable enforcement', entry: 'IPv4 / IPv6 / CIDR / domain', note: 'Customer / note', add: 'Add entry', save: 'Save entries and proxy settings only',
    cloudflare: 'Trust official Cloudflare CDN ranges', cdn: 'Other trusted CDN IPs / CIDRs', cdnHint: 'One per line; use origin addresses confirmed by your CDN provider',
    proxyHint: 'Configure local nginx / Docker peers in server.trusted_proxies. Proxies must overwrite client IP headers or append the actual peer. Forwarding headers from untrusted sources are ignored.',
    preview: 'Validate and preview DNS', testIP: 'Customer egress IP', test: 'Test allowlist', allowed: 'IP matches the allowlist', denied: 'IP will be blocked when enabled', testHint: 'Tests evaluate global rules as enabled; each key’s own IP restrictions still apply independently.',
    observations: 'Observe: unmatched client egress addresses', observationHint: 'Aggregated by IP, user and key; retained for 7 days, up to 10,000 groups. Redis outages may cause missing observations; verify traffic before enabling.',
    user: 'User', key: 'Key name / ID', count: 'Requests', lastSeen: 'Last request', addObserved: 'Add and save', refresh: 'Refresh observations', empty: 'No unmatched sources recorded',
    confirm: 'With saved rules, requests from {ips} IPs, {users} users and {keys} keys in the last 24 hours would be blocked. Once enabled, requests must pass both the global and per-key allowlists. Enable now?', failed: 'Operation failed; please retry',
  },
}
