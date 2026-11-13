package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCanTransitionMassEmailStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		from string
		to   string
		want bool
	}{
		{MassEmailStatusDraft, MassEmailStatusScheduled, true},
		{MassEmailStatusDraft, MassEmailStatusSending, true},
		{MassEmailStatusDraft, MassEmailStatusCancelled, true},
		{MassEmailStatusScheduled, MassEmailStatusSending, true},
		{MassEmailStatusScheduled, MassEmailStatusDraft, true},
		{MassEmailStatusSending, MassEmailStatusCompleted, true},
		{MassEmailStatusSending, MassEmailStatusCancelled, true},
		{MassEmailStatusFailed, MassEmailStatusSending, true},
		{MassEmailStatusSending, MassEmailStatusSending, true},
		{MassEmailStatusCompleted, MassEmailStatusSending, false},
		{MassEmailStatusCancelled, MassEmailStatusSending, false},
		{MassEmailStatusCompleted, MassEmailStatusDraft, false},
		{MassEmailStatusDraft, "unknown", false},
		{"", MassEmailStatusSending, false},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, CanTransitionMassEmailStatus(tt.from, tt.to), "%s -> %s", tt.from, tt.to)
	}
}

func TestIsMassEmailTerminalStatus(t *testing.T) {
	t.Parallel()
	require.True(t, IsMassEmailTerminalStatus(MassEmailStatusCompleted))
	require.True(t, IsMassEmailTerminalStatus(MassEmailStatusCancelled))
	require.False(t, IsMassEmailTerminalStatus(MassEmailStatusSending))
	require.False(t, IsMassEmailTerminalStatus(MassEmailStatusScheduled))
}

func TestIsValidMassEmailAddress(t *testing.T) {
	t.Parallel()

	valid := []string{
		"alice@example.com",
		"a.b+c@sub.example.co.uk",
		"UPPER@Example.COM",
		"  padded@example.com  ",
	}
	for _, email := range valid {
		require.True(t, IsValidMassEmailAddress(email), email)
	}

	invalid := []string{
		"",
		"   ",
		"bad-email",
		"a@b",
		"a@b.",
		"@example.com",
		"alice@",
		"alice@@example.com",
	}
	for _, email := range invalid {
		require.False(t, IsValidMassEmailAddress(email), email)
	}
}

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()
	require.Equal(t, "alice@example.com", NormalizeEmail("  Alice@Example.COM "))
}

func TestParseMassEmailManualInput(t *testing.T) {
	t.Parallel()

	raw := "Alice@Example.com\nbob@example.com, alice@example.com；carol@example.com\r\nbad-email"
	valid, invalid := ParseMassEmailManualInput(raw)

	require.Equal(t, []string{"alice@example.com", "bob@example.com", "carol@example.com"}, valid)
	require.Equal(t, []string{"bad-email"}, invalid)
}

func TestParseMassEmailManualInputEmpty(t *testing.T) {
	t.Parallel()
	valid, invalid := ParseMassEmailManualInput("  \n , ; ")
	require.Empty(t, valid)
	require.Empty(t, invalid)
}

func TestNormalizeMassEmailListKeepsFirstOccurrenceOrder(t *testing.T) {
	t.Parallel()
	valid, invalid := NormalizeMassEmailList([]string{"B@x.com", "a@x.com", "b@X.com", "", "nope"})
	require.Equal(t, []string{"b@x.com", "a@x.com"}, valid)
	require.Equal(t, []string{"nope"}, invalid)
}

func TestDedupeMassEmailCandidates(t *testing.T) {
	t.Parallel()

	userID := int64(7)
	enterpriseID := int64(3)
	candidates := []MassEmailRecipientCandidate{
		{Email: "Alice@Example.com", Name: "Alice", UserID: &userID, Source: MassEmailSourceUser},
		{Email: "alice@example.com", Name: "Alice Duplicate", Source: MassEmailSourceManual},
		{Email: "boss@example.com", Name: " 王总 ", EnterpriseID: &enterpriseID, Source: MassEmailSourceEnterpriseContact},
		{Email: "invalid", Name: "bad"},
		{Email: "  ", Name: "empty"},
	}
	out := DedupeMassEmailCandidates(candidates)

	require.Len(t, out, 2)
	require.Equal(t, "alice@example.com", out[0].Email)
	require.Equal(t, "Alice", out[0].Name, "首次出现的条目字段优先")
	require.Equal(t, MassEmailSourceUser, out[0].Source)
	require.Equal(t, "王总", out[1].Name, "名称应去除首尾空白")
}

func TestMassEmailRecipientFilterNormalizeAndValidate(t *testing.T) {
	t.Parallel()

	filter := MassEmailRecipientFilter{
		UserFilter: MassEmailUserFilter{
			Statuses: []string{" active ", "", "active"},
			Roles:    []string{" user "},
			GroupIDs: []int64{3, 0, -1, 3, 7},
			Search:   "  alice  ",
		},
		EnterpriseFilter: MassEmailEnterpriseFilter{EnterpriseIDs: []int64{5, 5, 2}},
		ManualEmails:     []string{"A@X.com", "a@x.com", "bad"},
	}
	filter.Normalize()

	require.Equal(t, []string{"active"}, filter.UserFilter.Statuses)
	require.Equal(t, []string{"user"}, filter.UserFilter.Roles)
	require.Equal(t, []int64{3, 7}, filter.UserFilter.GroupIDs)
	require.Equal(t, "alice", filter.UserFilter.Search)
	require.Equal(t, []int64{2, 5}, filter.EnterpriseFilter.EnterpriseIDs)
	require.Empty(t, filter.EnterpriseFilter.Scope, "未指定 scope 时保持空串")
	require.Equal(t, MassEmailScopeBoth, filter.EnterpriseFilter.NormalizedScope(), "未指定 scope 时按 both 处理")
	// 手动邮箱只做 trim/小写/去重：非法项留给解析阶段回显 invalid_emails，不在这里丢弃。
	require.Equal(t, []string{"a@x.com", "bad"}, filter.ManualEmails)

	require.NoError(t, filter.ValidateSyntax())
	require.False(t, filter.IsEmpty())
}

func TestMassEmailUserFilterHasConditions(t *testing.T) {
	t.Parallel()

	// 反序列化 null / {} / 未传都会得到零值，语义必须是"不选用户"。
	require.False(t, (MassEmailUserFilter{}).HasConditions())
	require.True(t, (MassEmailUserFilter{}).IsEmpty())

	from := time.Now()
	for name, filter := range map[string]MassEmailUserFilter{
		"statuses":     {Statuses: []string{"active"}},
		"roles":        {Roles: []string{"user"}},
		"group_ids":    {GroupIDs: []int64{3}},
		"search":       {Search: "alice"},
		"blank search": {},
		"created_from": {CreatedFrom: &from},
		"created_to":   {CreatedTo: &from},
	} {
		if name == "blank search" {
			filter.Search = "   "
			require.False(t, filter.HasConditions(), name)
			continue
		}
		require.True(t, filter.HasConditions(), name)
		require.False(t, filter.IsEmpty(), name)
	}

	// include_all_users=true 时才解析全部用户；仅有空 user_filter 不算。
	require.True(t, MassEmailUserFilter{}.WantsAllUsers(true))
	require.False(t, MassEmailUserFilter{}.WantsAllUsers(false))
	require.True(t, MassEmailUserFilter{Search: "alice"}.WantsAllUsers(false))
}

func TestMassEmailRecipientFilterMissingUserFilterMeansNoUsers(t *testing.T) {
	t.Parallel()

	// 模拟前端只选了企业 / 手动邮箱：user_filter 为 {} 或 null。
	raw := []byte(`{"include_all_users":false,"user_filter":null,"enterprise_filter":{"enterprise_ids":[4]},"manual_emails":[]}`)
	var filter MassEmailRecipientFilter
	require.NoError(t, json.Unmarshal(raw, &filter))
	filter.Normalize()

	require.False(t, filter.UserFilter.HasConditions())
	require.False(t, filter.UserFilter.WantsAllUsers(filter.IncludeAllUsers))
	require.NoError(t, filter.ValidateSyntax())
	require.False(t, filter.IsEmpty(), "企业来源仍算有来源")
}

func TestMassEmailRecipientFilterValidateSyntaxAllowsEmptyForDraft(t *testing.T) {
	t.Parallel()
	// 存草稿允许暂时没有收件人来源，"解析后 0 个收件人" 由 service 在发送/创建时判断。
	filter := MassEmailRecipientFilter{}
	filter.Normalize()
	require.True(t, filter.IsEmpty())
	require.NoError(t, filter.ValidateSyntax())
}

func TestMassEmailRecipientFilterValidateRejectsBadScope(t *testing.T) {
	t.Parallel()
	filter := MassEmailRecipientFilter{
		EnterpriseFilter: MassEmailEnterpriseFilter{EnterpriseIDs: []int64{1}, Scope: "everyone"},
	}
	filter.Normalize()
	require.ErrorIs(t, filter.ValidateSyntax(), ErrMassEmailInvalidFilter)
}

func TestMassEmailEnterpriseFilterScopeHelpers(t *testing.T) {
	t.Parallel()

	both := MassEmailEnterpriseFilter{}
	require.Equal(t, MassEmailScopeBoth, both.NormalizedScope())
	require.True(t, both.WantsContacts())
	require.True(t, both.WantsMembers())

	contacts := MassEmailEnterpriseFilter{Scope: "CONTACTS"}
	require.True(t, contacts.WantsContacts())
	require.False(t, contacts.WantsMembers())

	members := MassEmailEnterpriseFilter{Scope: MassEmailScopeMembers}
	require.False(t, members.WantsContacts())
	require.True(t, members.WantsMembers())

	require.Empty(t, MassEmailEnterpriseFilter{Scope: "nope"}.NormalizedScope())
}

func TestMassEmailRecipientFilterMarshalJSONUsesEmptyArrays(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(MassEmailRecipientFilter{IncludeAllUsers: true})
	require.NoError(t, err)
	require.Contains(t, string(raw), `"manual_emails":[]`)
	require.Contains(t, string(raw), `"include_all_users":true`)
}

func TestMassEmailCampaignRates(t *testing.T) {
	t.Parallel()

	campaign := &MassEmailCampaign{TotalCount: 100, SentCount: 60, FailedCount: 10, OpenedCount: 25}
	require.InDelta(t, 0.25, campaign.OpenRate(), 1e-9)
	require.InDelta(t, 0.7, campaign.Progress(), 1e-9)

	empty := &MassEmailCampaign{}
	require.Zero(t, empty.OpenRate())
	require.Zero(t, empty.Progress())

	overflow := &MassEmailCampaign{TotalCount: 10, SentCount: 9, FailedCount: 9}
	require.InDelta(t, 1.0, overflow.Progress(), 1e-9)
}

func TestMassEmailRecipientIsOpened(t *testing.T) {
	t.Parallel()
	opened := time.Now()
	require.True(t, (&MassEmailRecipient{OpenCount: 2, FirstOpenedAt: &opened}).IsOpened())
	require.False(t, (&MassEmailRecipient{}).IsOpened())
}

func TestMassEmailClaimLeaseAndRateLimitDefaults(t *testing.T) {
	t.Parallel()
	require.Equal(t, 5*time.Minute, MassEmailClaimLease)
	require.Equal(t, 2, MassEmailDefaultRateLimitPerSecond)
}
