//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type enterpriseAccessRepo struct {
	EnterpriseRepository
	membership *EnterpriseMemberContext
	err        error
	calls      int
}

func (r *enterpriseAccessRepo) GetMemberContextByUserID(context.Context, int64) (*EnterpriseMemberContext, error) {
	r.calls++
	return r.membership, r.err
}

type enterpriseAccessGroupRepo struct {
	GroupRepository
	groups []Group
}

func (r *enterpriseAccessGroupRepo) GetByID(_ context.Context, id int64) (*Group, error) {
	for _, group := range r.groups {
		if group.ID == id {
			return &group, nil
		}
	}
	return nil, ErrGroupNotFound
}

func (r *enterpriseAccessGroupRepo) ListActive(context.Context) ([]Group, error) {
	return r.groups, nil
}

type enterpriseAccessSubscriptionRepo struct {
	UserSubscriptionRepository
	groups []int64
	calls  int
}

func (r *enterpriseAccessSubscriptionRepo) GetActiveByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*UserSubscription, error) {
	r.calls++
	for _, id := range r.groups {
		if id == groupID {
			return &UserSubscription{UserID: userID, GroupID: id, Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)}, nil
		}
	}
	return nil, ErrSubscriptionNotFound
}

func (r *enterpriseAccessSubscriptionRepo) ListActiveByUserID(_ context.Context, userID int64) ([]UserSubscription, error) {
	out := make([]UserSubscription, len(r.groups))
	for i, id := range r.groups {
		out[i] = UserSubscription{UserID: userID, GroupID: id}
	}
	return out, nil
}

func TestEnterpriseUserCanBindGroup(t *testing.T) {
	for _, tc := range []struct {
		name       string
		membership EnterpriseMemberContext
		personal   User
		exclusive  bool
		want       bool
	}{
		{name: "pool grants exclusive group without personal grant", membership: EnterpriseMemberContext{GroupPoolConfigured: true, GroupPoolIDs: []int64{42}}, personal: User{RestrictPublicGroups: true}, exclusive: true, want: true},
		{name: "pool excludes personally granted public group", membership: EnterpriseMemberContext{GroupPoolConfigured: true, GroupPoolIDs: []int64{7}}, personal: User{AllowedGroups: []int64{42}}, want: false},
		{name: "empty pool falls back to unrestricted public rule", membership: EnterpriseMemberContext{}, want: true},
		{name: "empty pool preserves exclusive personal grants", membership: EnterpriseMemberContext{}, personal: User{AllowedGroups: []int64{42}}, exclusive: true, want: true},
		{name: "empty pool never grants exclusive group itself", membership: EnterpriseMemberContext{}, exclusive: true, want: false},
		{name: "empty pool preserves public restriction", membership: EnterpriseMemberContext{}, personal: User{RestrictPublicGroups: true}, want: false},
		{name: "whitelist narrows personal public rule", membership: EnterpriseMemberContext{GroupWhitelistConfigured: true, GroupWhitelistIDs: []int64{7}}, want: false},
		{name: "configured empty intersection denies all", membership: EnterpriseMemberContext{GroupPoolConfigured: true, GroupPoolIDs: []int64{42}, GroupWhitelistConfigured: true}, want: false},
		{name: "pool and whitelist intersection grants", membership: EnterpriseMemberContext{GroupPoolConfigured: true, GroupPoolIDs: []int64{42}, GroupWhitelistConfigured: true, GroupWhitelistIDs: []int64{42}}, exclusive: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := tc.personal
			user.Enterprise = &tc.membership
			require.Equal(t, tc.want, user.CanBindGroup(42, tc.exclusive))
		})
	}
}

func TestEnterpriseAPIKeyGroupAccessLoadsMembershipAndFiltersSubscriptions(t *testing.T) {
	ctx := context.Background()
	groups := []Group{
		{ID: 1, SubscriptionType: SubscriptionTypeStandard},
		{ID: 2, IsExclusive: true, SubscriptionType: SubscriptionTypeStandard},
		{ID: 3, SubscriptionType: SubscriptionTypeSubscription},
		{ID: 4, SubscriptionType: SubscriptionTypeSubscription},
		{ID: 5, SubscriptionType: SubscriptionTypeSubscription},
	}
	personal := &User{ID: 8, RestrictPublicGroups: true, AllowedGroups: []int64{1}}
	repo := &enterpriseAccessRepo{membership: &EnterpriseMemberContext{GroupPoolConfigured: true, GroupPoolIDs: []int64{2, 3, 5}}}
	subs := &enterpriseAccessSubscriptionRepo{groups: []int64{3, 4}}
	svc := &APIKeyService{userRepo: &visibilityUserRepo{user: personal}, groupRepo: &enterpriseAccessGroupRepo{groups: groups}, userSubRepo: subs}
	svc.SetEnterpriseRepository(repo)
	available, err := svc.GetAvailableGroups(ctx, personal.ID)
	require.NoError(t, err)
	require.Equal(t, []Group{groups[1], groups[2]}, available)
	require.Equal(t, 1, repo.calls)
	require.Nil(t, personal.Enterprise, "membership hydration must not mutate repository user objects")
	visible, restricted, err := svc.GetUserGroupVisibility(ctx, personal.ID)
	require.NoError(t, err)
	require.True(t, restricted)
	require.Equal(t, map[int64]struct{}{2: {}, 3: {}}, visible)
	user, err := svc.getUserForGroupAccess(ctx, personal.ID)
	require.NoError(t, err)
	for _, group := range groups {
		want := group.ID == 2 || group.ID == 3
		require.Equal(t, want, svc.canUserBindGroup(ctx, user, &group), "group %d", group.ID)
	}
	groupID := int64(1)
	_, err = svc.Create(ctx, personal.ID, CreateAPIKeyRequest{Name: "denied", GroupID: &groupID})
	require.ErrorIs(t, err, ErrGroupNotAllowed)
}

func TestEnterpriseAPIKeyMembershipLoadFailureIsClosed(t *testing.T) {
	failure := errors.New("membership database unavailable")
	svc := &APIKeyService{userRepo: &visibilityUserRepo{user: &User{ID: 1}}}
	svc.SetEnterpriseRepository(&enterpriseAccessRepo{err: failure})
	_, err := svc.Create(context.Background(), 1, CreateAPIKeyRequest{})
	require.ErrorIs(t, err, failure)
	_, err = svc.GetAvailableGroups(context.Background(), 1)
	require.ErrorIs(t, err, failure)
	_, _, err = svc.GetUserGroupVisibility(context.Background(), 1)
	require.ErrorIs(t, err, failure)
}

func TestEnterpriseAPIKeyAuthSnapshotRoundTrip(t *testing.T) {
	departmentID := int64(77)
	membership := &EnterpriseMemberContext{EnterpriseID: 42, EnterpriseName: "Test", EnterpriseStatus: EnterpriseStatusDisabled, MemberID: 5, MemberStatus: EnterpriseMemberStatusDisabled, Role: EnterpriseRoleManager, DepartmentID: &departmentID, GroupPoolConfigured: true, GroupPoolIDs: []int64{1, 2}, GroupWhitelistConfigured: true, GroupWhitelistIDs: []int64{2}}
	key := &APIKey{ID: 1, UserID: 8, User: &User{ID: 8, Enterprise: membership}}
	svc := &APIKeyService{}
	snapshot := svc.snapshotFromAPIKey(context.Background(), key)
	require.Equal(t, 25, snapshot.Version)
	require.Equal(t, membership, snapshot.User.Enterprise)
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	var decoded APIKeyAuthSnapshot
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	roundTrip := svc.snapshotToAPIKey("key", &decoded)
	require.Equal(t, membership, roundTrip.User.Enterprise)
	roundTrip.User.Enterprise.GroupPoolIDs[0] = 99
	*roundTrip.User.Enterprise.DepartmentID = 99
	require.Equal(t, int64(1), snapshot.User.Enterprise.GroupPoolIDs[0])
	require.Equal(t, int64(77), *snapshot.User.Enterprise.DepartmentID)
	_, accepted, err := svc.applyAuthCacheEntry("old", &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{Version: 24}})
	require.NoError(t, err)
	require.False(t, accepted, "pre-enterprise snapshots must be reloaded before authentication")
	key.User.Enterprise = nil
	require.Nil(t, svc.snapshotToAPIKey("personal", svc.snapshotFromAPIKey(context.Background(), key)).User.Enterprise)
}
