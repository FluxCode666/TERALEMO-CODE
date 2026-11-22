# 告警通知 Webhook

在管理后台 **系统设置 → 邮件设置 → 告警通知渠道** 添加自定义 Webhook。建议使用 HTTPS，并为每个渠道设置独立的签名密钥。默认仅允许公网出站目标；私有接收端需由运维加入 `security.outbound.allow_private_targets`。请求不会跟随重定向。

## 请求格式

服务端发送 `POST`，`Content-Type: application/json`，`User-Agent: AlertNotify/1.0`。示例：

```json
{
  "version": "1",
  "event": "ops.alert",
  "status": "firing",
  "severity": "critical",
  "title": "错误率告警",
  "summary": "告警触发：错误率告警",
  "fields": [{"label": "规则名", "value": "错误率告警"}],
  "occurred_at": "2026-11-22T14:00:00Z",
  "site_name": "Sub2API",
  "source": {"type": "ops_alert", "id": "123"},
  "test": false
}
```

- `ops.alert` 的 `status` 为 `firing` 或 `resolved`；恢复消息仅在评估器自动恢复且渠道开启恢复通知时发送。
- `account.quota_alert` 在账号自身配额越线时发送；`status` 为 `firing`、`severity` 为 `warning`，`source.type` 为 `account_quota`，`source.id` 为 `<账号ID>-<维度>`。
- 测试消息为 `event: "test"`、`status: "firing"`、`severity: "info"`、`test: true`。

飞书和 Webhook 渠道独立于邮件通知启用；即使关闭告警邮件或未配置管理员邮箱，订阅了对应事件的启用渠道仍可接收通知。

接收端应快速返回 HTTP `2xx`。自动告警在网络错误或 `5xx` 时最多重试一次；接收端应容忍重复投递。投递不保证必达或有序。

## 验证签名

- `X-Webhook-Event` 与 JSON 中的 `event` 一致。
- `X-Webhook-Timestamp` 是发送时的 Unix 秒字符串，不是 `occurred_at`。
- 配置签名密钥后，`X-Webhook-Signature` 为 `sha256=<小写 hex>`，值是 `HMAC-SHA256(secret, timestamp + "." + 原始请求体)`。

用未经 JSON 解析或重新序列化的原始请求体计算签名，并用恒定时间比较。下面的 Go 示例允许 5 分钟时钟偏差；短时间内的重复请求仍需接收端按需去重。

```go
import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "strconv"
    "strings"
    "time"
)

func verifyAlertWebhook(secret, timestamp, signature string, body []byte) bool {
    if secret == "" {
        return false
    }
    unix, err := strconv.ParseInt(timestamp, 10, 64)
    if err != nil {
        return false
    }
    age := time.Since(time.Unix(unix, 0))
    if age > 5*time.Minute || age < -5*time.Minute {
        return false
    }
    encoded, ok := strings.CutPrefix(signature, "sha256=")
    if !ok {
        return false
    }
    received, err := hex.DecodeString(encoded)
    if err != nil {
        return false
    }
    mac := hmac.New(sha256.New, []byte(secret))
    _, _ = mac.Write([]byte(timestamp + "."))
    _, _ = mac.Write(body)
    return hmac.Equal(received, mac.Sum(nil))
}
```
