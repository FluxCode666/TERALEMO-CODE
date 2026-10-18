export default {
  "enterprises": {
    "memberConcurrency": {
      "title": "成员并发",
      "description": "统一设置每位成员的并发上限，包括停用成员，不含已移出成员。仅系统管理员可修改。",
      "current": "当前统一并发：{value}",
      "unset": "未统一设置，新成员使用平台默认并发",
      "input": "每位成员的并发上限",
      "validation": "请输入 1～2147483647 之间的正整数。",
      "apply": "应用到全部成员",
      "clear": "清除统一设置",
      "confirmApply": "将把 {count} 名当前成员（含停用成员，不含已移出成员）的并发都改为 {value}，且之后新成员默认使用该值。确认后立即生效。",
      "confirmClear": "清除后，现有成员的并发不变，之后新成员回到平台默认并发。确认后立即生效。",
      "updated": "已更新 {count} 名成员",
      "cleared": "已清除统一设置，现有成员并发不变，新成员使用平台默认并发。"
    },
    "title": "企业管理",
    "description": "管理企业组织、余额与使用权限",
    "create": "创建企业",
    "createTitle": "创建企业",
    "initialOwner": "初始企业负责人",
    "noOwner": "暂不设置负责人",
    "noActiveOwner": "无可用负责人",
    "noActiveOwnerHint": "需在成员中指定一位启用的负责人",
    "open": "查看详情",
    "delete": "删除企业",
    "confirmDelete": "确定删除此企业？",
    "dissolve": "解散企业",
    "confirmDissolve": "此企业仍有成员。确定解散企业？全部成员将恢复为个人用户，企业剩余余额随企业归档，不会转入个人钱包。",
    "searchPlaceholder": "搜索企业名称",
    "name": "企业名称",
    "status": "状态",
    "balance": "余额（USD）",
    "members": "成员数",
    "departments": "部门数",
    "createdAt": "创建时间",
    "detail": "企业详情"
  }
}
