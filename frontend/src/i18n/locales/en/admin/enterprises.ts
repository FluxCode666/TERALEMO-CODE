export default {
  "enterprises": {
    "memberConcurrency": {
      "title": "Member concurrency",
      "description": "Set a concurrency limit for each member, including disabled members and excluding removed members. Only system administrators can change this setting.",
      "current": "Current unified concurrency: {value}",
      "unset": "No unified setting. New members use the platform default concurrency.",
      "input": "Concurrency limit per member",
      "validation": "Enter a positive integer between 1 and 2147483647.",
      "apply": "Apply to all members",
      "clear": "Clear unified setting",
      "confirmApply": "Change concurrency to {value} for all {count} current members, including disabled members and excluding removed members. New members will also use this value by default. This takes effect immediately after confirmation.",
      "confirmClear": "Existing members keep their current concurrency. New members will use the platform default concurrency. This takes effect immediately after confirmation.",
      "updated": "Updated {count} members",
      "cleared": "Unified setting cleared. Existing members keep their concurrency; new members use the platform default."
    },
    "title": "Enterprises",
    "description": "Manage enterprise organizations, balances, and access",
    "create": "Create enterprise",
    "createTitle": "Create enterprise",
    "initialOwner": "Initial enterprise owner",
    "noOwner": "No initial owner",
    "noActiveOwner": "No active owner",
    "noActiveOwnerHint": "Assign an enabled owner in the member list.",
    "open": "View details",
    "delete": "Delete enterprise",
    "confirmDelete": "Delete this enterprise?",
    "dissolve": "Dissolve enterprise",
    "confirmDissolve": "This enterprise still has members. Dissolve it? All members will return to personal accounts. The remaining enterprise balance will be archived with the enterprise and will not be transferred to personal wallets.",
    "searchPlaceholder": "Search enterprise name",
    "name": "Enterprise name",
    "status": "Status",
    "balance": "Balance (USD)",
    "members": "Members",
    "departments": "Departments",
    "createdAt": "Created at",
    "detail": "Enterprise details"
  }
}
