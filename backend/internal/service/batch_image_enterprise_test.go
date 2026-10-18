//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

type batchEnterpriseReaderStub struct {
	member *EnterpriseMemberContext
	err    error
}

func (s batchEnterpriseReaderStub) GetMemberContextByUserID(context.Context, int64) (*EnterpriseMemberContext, error) {
	return s.member, s.err
}

func enterpriseBatchSubmitFixture() (*BatchImagePublicService, *fakeBatchImageRepository, BatchImageOwner, []EnterpriseQuota) {
	svc, repo, _, _, _ := newTestBatchImagePublicService(true)
	user, quotas := enterpriseEligibilityFixture()
	owner := testBatchImageOwner()
	owner.GroupID, owner.Enterprise = enterpriseTestID(5), user.Enterprise
	svc.GroupRepo = &publicBatchImageGroupRepo{groups: map[int64]*Group{5: {ID: 5, Platform: PlatformGemini, AllowBatchImageGeneration: true, RateMultiplier: 1, BatchImageDiscountMultiplier: .5, BatchImageHoldMultiplier: .6}}}
	svc.EnterpriseRepo = batchEnterpriseReaderStub{member: user.Enterprise}
	svc.BillingCache = &BillingCacheService{enterpriseCache: &enterpriseRuntimeCacheStub{balance: 100, quotas: quotas}}
	return svc, repo, owner, quotas
}

func TestBatchImagesEnterpriseAdmission(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*BatchImagePublicService, BatchImageOwner)
		want   error
	}{
		{"enabled", func(*BatchImagePublicService, BatchImageOwner) {}, nil},
		{"enterprise disabled", func(_ *BatchImagePublicService, o BatchImageOwner) {
			o.Enterprise.EnterpriseStatus = EnterpriseStatusDisabled
		}, ErrEnterpriseGatewayDisabled},
		{"member disabled", func(_ *BatchImagePublicService, o BatchImageOwner) {
			o.Enterprise.MemberStatus = EnterpriseMemberStatusDisabled
		}, ErrEnterpriseGatewayMemberDisabled},
		{"group denied", func(_ *BatchImagePublicService, o BatchImageOwner) { o.Enterprise.GroupWhitelistConfigured = true }, ErrEnterpriseGatewayGroupNotAllowed},
		{"membership unavailable", func(s *BatchImagePublicService, _ BatchImageOwner) {
			s.EnterpriseRepo = batchEnterpriseReaderStub{err: errors.New("unavailable")}
		}, ErrBillingServiceUnavailable},
		{"insufficient available balance", func(s *BatchImagePublicService, _ BatchImageOwner) {
			s.BillingRepo.(*fakeBatchImageBillingRepo).reserveErr = ErrEnterpriseBillingBalanceInsufficient
		}, ErrEnterpriseBillingBalanceInsufficient},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, owner, _ := enterpriseBatchSubmitFixture()
			tc.change(svc, owner)
			result, err := svc.Submit(context.Background(), owner, validBatchImageSubmitRequest(), "")
			require.ErrorIs(t, err, tc.want)
			billing := svc.BillingRepo.(*fakeBatchImageBillingRepo)
			if tc.want != nil {
				if tc.name != "insufficient available balance" {
					require.Empty(t, billing.reserves)
					require.Empty(t, repo.jobs)
				}
				return
			}
			job := repo.jobs[result.ID]
			require.Equal(t, &owner.Enterprise.EnterpriseID, job.EnterpriseID)
			require.Equal(t, owner.Enterprise.DepartmentID, job.EnterpriseDepartmentID)
			require.Equal(t, &owner.Enterprise.MemberID, job.EnterpriseMemberID)
			require.Equal(t, owner.Enterprise.EnterpriseID, billing.reserves[0].EnterpriseID)
			// Later changes to membership cannot change an already persisted payer.
			cmd, err := buildBatchImageHoldCommand(job, BatchImageCaptureRequestID(job.BatchID), .1, "manifest")
			require.NoError(t, err)
			require.Equal(t, *job.EnterpriseID, cmd.EnterpriseID)
			require.Equal(t, *job.EnterpriseDepartmentID, cmd.EnterpriseDepartmentID)
			require.Equal(t, *job.EnterpriseMemberID, cmd.EnterpriseMemberID)
		})
	}
}

func TestBatchImagesEnterpriseAdmissionAllQuotaScopesAndWindows(t *testing.T) {
	for _, scope := range []string{EnterpriseScopeEnterprise, EnterpriseScopeDepartment, EnterpriseScopeMember} {
		for _, window := range []string{EnterpriseQuotaWindowDaily, EnterpriseQuotaWindowWeekly, EnterpriseQuotaWindowMonthly, EnterpriseQuotaWindowTotal} {
			t.Run(scope+"/"+window, func(t *testing.T) {
				svc, repo, owner, quotas := enterpriseBatchSubmitFixture()
				now, limit := timezone.Now(), 1.0
				for i := range quotas {
					q := &quotas[i]
					if q.ScopeType != scope {
						continue
					}
					switch window {
					case EnterpriseQuotaWindowDaily:
						q.DailyLimitUSD, q.DailyUsageUSD, q.DailyWindowStart = &limit, limit, &now
					case EnterpriseQuotaWindowWeekly:
						q.WeeklyLimitUSD, q.WeeklyUsageUSD, q.WeeklyWindowStart = &limit, limit, &now
					case EnterpriseQuotaWindowMonthly:
						q.MonthlyLimitUSD, q.MonthlyUsageUSD, q.MonthlyWindowStart = &limit, limit, &now
					case EnterpriseQuotaWindowTotal:
						q.TotalLimitUSD, q.TotalUsageUSD = &limit, limit
					}
				}
				_, err := svc.Submit(context.Background(), owner, validBatchImageSubmitRequest(), "")
				want := ErrEnterpriseBillingQuotaExceeded
				if scope == EnterpriseScopeDepartment {
					want = ErrEnterpriseBillingDepartmentQuotaExceeded
				}
				if scope == EnterpriseScopeMember {
					want = ErrEnterpriseBillingMemberQuotaExceeded
				}
				require.ErrorIs(t, err, want)
				require.Empty(t, repo.jobs)
				require.Empty(t, svc.BillingRepo.(*fakeBatchImageBillingRepo).reserves)
			})
		}
	}
}

func TestBatchImagesSubmissionRefreshesPayerAndLegacyPersonalSnapshot(t *testing.T) {
	svc, repo, owner, _ := enterpriseBatchSubmitFixture()
	owner.Enterprise = nil // Authentication predates joining the enterprise.
	result, err := svc.Submit(context.Background(), owner, validBatchImageSubmitRequest(), "")
	require.NoError(t, err)
	require.NotNil(t, repo.jobs[result.ID].EnterpriseID)
	// A historical personal job always produces a personal command and log.
	job := testSettlingBatchImageJob("historical-personal")
	cmd, err := buildBatchImageHoldCommand(job, BatchImageCaptureRequestID(job.BatchID), .1, "")
	require.NoError(t, err)
	require.Zero(t, cmd.EnterpriseID)
	log := buildBatchImageUsageLog(job, .1, cmd.RequestID, timezone.Now())
	require.Nil(t, log.EnterpriseID)
	require.Nil(t, log.EnterpriseDepartmentID)
}
