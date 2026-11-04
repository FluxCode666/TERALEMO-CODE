//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type manualSchedulePauseRepo struct {
	accountRepoStubForBulkUpdate
	states       map[int64][]*ScheduledQuestionTestAccountState
	clearIDs     []int64
	clearErrByID map[int64]error
	setErr       error
}

func (r *manualSchedulePauseRepo) SetSchedulable(_ context.Context, id int64, schedulable bool) error {
	if r.setErr != nil {
		return r.setErr
	}
	r.getByIDAccounts[id].Schedulable = schedulable
	return nil
}

func (r *manualSchedulePauseRepo) SetError(_ context.Context, id int64, message string) error {
	if r.setErr != nil {
		return r.setErr
	}
	account := r.getByIDAccounts[id]
	account.Status, account.ErrorMessage, account.Schedulable = StatusError, message, false
	return nil
}

func (r *manualSchedulePauseRepo) ClearScheduledQuestionAutoPause(_ context.Context, id int64) error {
	r.clearIDs = append(r.clearIDs, id)
	if err := r.clearErrByID[id]; err != nil {
		return err
	}
	for _, state := range r.states[id] {
		state.AutoPaused, state.AutoPausedAt = false, nil
	}
	return nil
}

type manualScheduleBillingRepo struct {
	*manualSchedulePauseRepo
	billingUpdates int
}

func (r *manualScheduleBillingRepo) UpdateWithAccountBillingSettings(ctx context.Context, account *Account, _, _ *bool, _ *float64) error {
	r.billingUpdates++
	return r.Update(ctx, account)
}

func newManualSchedulePauseRepo() *manualSchedulePauseRepo {
	now := time.Now()
	r := &manualSchedulePauseRepo{
		states:       make(map[int64][]*ScheduledQuestionTestAccountState),
		clearErrByID: make(map[int64]error),
	}
	r.getByIDAccounts = make(map[int64]*Account)
	for _, id := range []int64{1, 2, 3} {
		r.getByIDAccounts[id] = &Account{ID: id, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive}
		r.states[id] = []*ScheduledQuestionTestAccountState{
			{PlanID: 10, AccountID: id, State: "alert", AutoPaused: true, AutoPausedAt: &now},
			{PlanID: 20, AccountID: id, State: "alert", AutoPaused: true, AutoPausedAt: &now},
		}
	}
	return r
}

func TestAdminAccountSchedulableReleasesAllPlanOwnership(t *testing.T) {
	for _, value := range []bool{false, true} {
		t.Run(map[bool]string{false: "pause", true: "resume"}[value], func(t *testing.T) {
			repo := newManualSchedulePauseRepo()
			svc := &adminServiceImpl{accountRepo: repo}
			updated, err := svc.SetAccountSchedulable(context.Background(), 1, value)
			require.NoError(t, err)
			require.Equal(t, value, updated.Schedulable)
			require.Equal(t, []int64{1}, repo.clearIDs)
			for _, state := range repo.states[1] {
				require.False(t, state.AutoPaused)
				require.Nil(t, state.AutoPausedAt)
				require.Equal(t, "alert", state.State)
			}
			require.True(t, repo.states[2][0].AutoPaused)
		})
	}
}

func TestAdminAccountSchedulableDoesNotHideOwnershipClearFailure(t *testing.T) {
	failure := errors.New("ownership update unavailable")
	repo := newManualSchedulePauseRepo()
	repo.clearErrByID[1] = failure
	svc := &adminServiceImpl{accountRepo: repo}
	updated, err := svc.SetAccountSchedulable(context.Background(), 1, true)
	require.Nil(t, updated)
	require.ErrorIs(t, err, failure)
	require.Contains(t, err.Error(), "scheduling updated but failed")
	require.True(t, repo.getByIDAccounts[1].Schedulable)
	require.True(t, repo.states[1][0].AutoPaused)

	repo = newManualSchedulePauseRepo()
	repo.setErr = failure
	svc.accountRepo = repo
	_, err = svc.SetAccountSchedulable(context.Background(), 1, true)
	require.ErrorIs(t, err, failure)
	require.Empty(t, repo.clearIDs, "failed writes must not release ownership")
}

func TestAdminAccountErrorReleasesOwnershipAndPropagatesClearFailure(t *testing.T) {
	repo := newManualSchedulePauseRepo()
	svc := &adminServiceImpl{accountRepo: repo}
	require.NoError(t, svc.SetAccountError(context.Background(), 1, "manual error"))
	require.Equal(t, []int64{1}, repo.clearIDs)
	require.False(t, repo.states[1][0].AutoPaused)
	repo.clearErrByID[2] = errors.New("clear unavailable")
	require.ErrorIs(t, svc.SetAccountError(context.Background(), 2, "manual error"), repo.clearErrByID[2])
	require.True(t, repo.states[2][0].AutoPaused)
	repo.setErr = errors.New("set error failed")
	require.ErrorIs(t, svc.SetAccountError(context.Background(), 3, "manual error"), repo.setErr)
	require.Equal(t, []int64{1, 2}, repo.clearIDs)
}

func TestAdminEditAccountReleasesOwnershipOnlyForSchedulingWrites(t *testing.T) {
	enabled := true
	for _, test := range []struct {
		name         string
		input        *UpdateAccountInput
		wantClear    bool
		writeFailure bool
		clearFailure bool
	}{
		{name: "unrelated edit", input: &UpdateAccountInput{Name: "renamed"}},
		{name: "explicit false", input: &UpdateAccountInput{Schedulable: new(bool)}, wantClear: true},
		{name: "explicit true", input: &UpdateAccountInput{Schedulable: &enabled}, wantClear: true},
		{name: "error status implicitly pauses", input: &UpdateAccountInput{Status: StatusError}, wantClear: true},
		{name: "write failed", input: &UpdateAccountInput{Schedulable: new(bool)}, writeFailure: true},
		{name: "clear failed", input: &UpdateAccountInput{Schedulable: new(bool)}, wantClear: true, clearFailure: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newManualSchedulePauseRepo()
			failure := errors.New("injected failure")
			if test.writeFailure {
				repo.updateErr = failure
			}
			if test.clearFailure {
				repo.clearErrByID[1] = failure
			}
			svc := &adminServiceImpl{accountRepo: repo}
			updated, err := svc.UpdateAccount(context.Background(), 1, test.input)
			if test.writeFailure || test.clearFailure {
				require.Nil(t, updated)
				require.ErrorIs(t, err, failure)
			} else {
				require.NoError(t, err)
			}
			if test.wantClear {
				require.Equal(t, []int64{1}, repo.clearIDs)
			} else {
				require.Empty(t, repo.clearIDs)
			}
		})
	}
}

func TestAdminEditBillingSettingsPathAlsoReleasesOwnership(t *testing.T) {
	repo := &manualScheduleBillingRepo{manualSchedulePauseRepo: newManualSchedulePauseRepo()}
	svc := &adminServiceImpl{accountRepo: repo}
	_, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Schedulable: new(bool)})
	require.NoError(t, err)
	require.Equal(t, 1, repo.billingUpdates)
	require.Equal(t, []int64{1}, repo.clearIDs)
	require.False(t, repo.states[1][0].AutoPaused)
}

func TestAdminBulkSchedulableReleasesEveryAccountAndReportsClearFailures(t *testing.T) {
	for _, value := range []bool{false, true} {
		t.Run(map[bool]string{false: "pause", true: "resume"}[value], func(t *testing.T) {
			repo := newManualSchedulePauseRepo()
			repo.clearErrByID[2] = errors.New("ownership unavailable")
			svc := &adminServiceImpl{accountRepo: repo}
			result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1, 2, 3}, Schedulable: &value})
			require.NoError(t, err)
			require.Equal(t, []int64{1, 2, 3}, repo.clearIDs)
			require.Equal(t, []int64{1, 3}, result.SuccessIDs)
			require.Equal(t, []int64{2}, result.FailedIDs)
			require.Equal(t, 2, result.Success)
			require.Equal(t, 1, result.Failed)
			require.False(t, result.Results[1].Success)
			require.Contains(t, result.Results[1].Error, "ownership unavailable")
			require.False(t, repo.states[1][0].AutoPaused)
			require.True(t, repo.states[2][0].AutoPaused)
			require.False(t, repo.states[3][1].AutoPaused)
		})
	}
}

func TestAdminBulkNonSchedulingOrFailedWritePreservesOwnership(t *testing.T) {
	for _, failed := range []bool{false, true} {
		repo := newManualSchedulePauseRepo()
		input := &BulkUpdateAccountsInput{AccountIDs: []int64{1, 2}, Name: "renamed"}
		if failed {
			repo.bulkUpdateErr = errors.New("bulk update failed")
			input.Schedulable = new(bool)
		}
		svc := &adminServiceImpl{accountRepo: repo}
		_, err := svc.BulkUpdateAccounts(context.Background(), input)
		if failed {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
		require.Empty(t, repo.clearIDs)
		require.True(t, repo.states[1][0].AutoPaused)
	}
}

func TestAdminBulkFilterResolvedSchedulingReleasesOwnership(t *testing.T) {
	repo := newManualSchedulePauseRepo()
	repo.listData = []Account{{ID: 1}, {ID: 3}}
	repo.listResult = &pagination.PaginationResult{Total: 2}
	svc := &adminServiceImpl{accountRepo: repo}
	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		Filters: &BulkUpdateAccountFilters{Platform: PlatformAnthropic}, Schedulable: new(bool),
	})
	require.NoError(t, err)
	require.Equal(t, []int64{1, 3}, repo.clearIDs)
	require.Equal(t, []int64{1, 3}, result.SuccessIDs)
	require.True(t, repo.states[2][0].AutoPaused)
}
