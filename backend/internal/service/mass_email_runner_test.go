package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 限速器 / 批量
// ---------------------------------------------------------------------------

func TestMassEmailBatchSizeRespectsLease(t *testing.T) {
	t.Parallel()

	// 批量 × 单封超时必须小于 5 分钟领取租约（lead #42 B）。
	require.Less(t, massEmailBatchSize(50)*int(massEmailSendTimeout/time.Second), int(domain.MassEmailClaimLease/time.Second))
	require.Equal(t, massEmailMaxBatch, massEmailBatchSize(2))
	require.Equal(t, massEmailMaxBatch, massEmailBatchSize(10))
	require.Equal(t, massEmailMaxBatch, massEmailBatchSize(1), "1/s 时 rate×10 也被上限夹住")
	require.Equal(t, massEmailMaxBatch, massEmailBatchSize(0), "非法速率回落到默认 2/s")
}

func TestMassEmailSendTimeoutIsThirtySeconds(t *testing.T) {
	t.Parallel()
	require.Equal(t, 30*time.Second, massEmailSendTimeout)
}

func TestMassEmailRateLimiterSharedAcrossCampaigns(t *testing.T) {
	t.Parallel()

	// 同一个 limiter 实例被多个 campaign 共用：连续 3 次 Wait 的总耗时必须 ≥ 2 个间隔，
	// 证明限速是「账号级总量」而不是「每任务各一份」。
	limiter := newMassEmailRateLimiter(20) // 50ms 间隔
	require.Equal(t, 50*time.Millisecond, limiter.Interval())

	start := time.Now()
	for i := 0; i < 3; i++ {
		require.NoError(t, limiter.Wait(context.Background()))
	}
	elapsed := time.Since(start)
	require.GreaterOrEqual(t, elapsed, 95*time.Millisecond, "三次发送必须被拉平成至少两个间隔")
}

func TestMassEmailRateLimiterWaitHonoursContext(t *testing.T) {
	t.Parallel()

	limiter := newMassEmailRateLimiter(1) // 1s 间隔
	require.NoError(t, limiter.Wait(context.Background()))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.Error(t, limiter.Wait(ctx), "第二个时隙要等 1s，ctx 超时必须立刻返回")
}

func TestMassEmailRateLimiterSetRateClamps(t *testing.T) {
	t.Parallel()

	limiter := newMassEmailRateLimiter(2)
	limiter.SetRate(0)
	require.Equal(t, 500*time.Millisecond, limiter.Interval(), "0 回落到默认 2/s")

	limiter.SetRate(1000)
	require.Equal(t, time.Second/time.Duration(MaxMassEmailRateLimitPerSecond), limiter.Interval(), "超过上限被夹住")
}

// ---------------------------------------------------------------------------
// Runner 行为
// ---------------------------------------------------------------------------

func newMassEmailRunnerForTest(repo *fakeMassEmailRepo) *MassEmailRunner {
	runner := NewMassEmailRunner(repo, newMassEmailServiceForTest(repo), nil, nil, nil)
	// 单测里没有 Redis：本地模式直接持锁（真实环境由 Redis 锁 + 续期决定）。
	runner.leaderMu.Lock()
	runner.leaderHeld = true
	runner.lockDeadline = time.Now().Add(massEmailGlobalLockTTL)
	runner.newEpochLocked()
	runner.leaderMu.Unlock()
	return runner
}

func TestMassEmailRunnerStopsOnCancelledCampaign(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusCancelled}
	repo.recipients[1] = []*domain.MassEmailRecipient{
		{ID: 11, CampaignID: 1, Email: "a@example.com", Status: domain.MassEmailRecipientStatusPending},
	}

	newMassEmailRunnerForTest(repo).drain(context.Background(), 1)

	require.Equal(t, 0, repo.lastClaimLimit, "已取消的任务不应再领取收件人")
	require.Equal(t, domain.MassEmailRecipientStatusPending, repo.recipients[1][0].Status)
}

func TestMassEmailRunnerStopsWithoutLeadership(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusSending}
	repo.recipients[1] = []*domain.MassEmailRecipient{
		{ID: 11, CampaignID: 1, Email: "a@example.com", Status: domain.MassEmailRecipientStatusPending},
	}

	runner := NewMassEmailRunner(repo, newMassEmailServiceForTest(repo), nil, nil, nil)
	// 默认没有 leadership：没有全局锁的实例绝不能发信。
	runner.drain(context.Background(), 1)

	require.Equal(t, 0, repo.lastClaimLimit, "非 leader 不应领取任何收件人")
}

func TestMassEmailRunnerReleaseRemainingAlsoRollsBackAttempts(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	claimed := time.Now()
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusSending}
	repo.recipients[1] = []*domain.MassEmailRecipient{
		{ID: 11, CampaignID: 1, Email: "a@example.com", Status: domain.MassEmailRecipientStatusPending, ClaimedAt: &claimed, Attempts: 1},
		{ID: 12, CampaignID: 1, Email: "b@example.com", Status: domain.MassEmailRecipientStatusPending, ClaimedAt: &claimed, Attempts: 1},
	}

	newMassEmailRunnerForTest(repo).releaseRemaining(1, []domain.MassEmailRecipient{{ID: 11}})

	require.Nil(t, repo.recipients[1][0].ClaimedAt, "已领取未发送的收件人要放回队列")
	require.Equal(t, 0, repo.recipients[1][0].Attempts, "attempts 要减回，不能虚增")
	require.NotNil(t, repo.recipients[1][1].ClaimedAt, "不属于本批的收件人不受影响")
	require.Equal(t, 1, repo.recipients[1][1].Attempts)
}

func TestMassEmailRunnerTickPromotesDueScheduledCampaign(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	past := time.Now().Add(-time.Minute)
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusScheduled, ScheduledAt: &past}

	runner := newMassEmailRunnerForTest(repo)
	runner.tick(context.Background())

	require.Equal(t, domain.MassEmailStatusSending, repo.campaigns[1].Status, "到点的定时任务应被推进到 sending")
}

func TestMassEmailRunnerTickIgnoresFutureScheduledCampaign(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	future := time.Now().Add(time.Hour)
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusScheduled, ScheduledAt: &future}

	runner := newMassEmailRunnerForTest(repo)
	runner.tick(context.Background())

	require.Equal(t, domain.MassEmailStatusScheduled, repo.campaigns[1].Status)
}

func TestMassEmailRunnerTickStopsWhenNotLeader(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	past := time.Now().Add(-time.Minute)
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusScheduled, ScheduledAt: &past}

	runner := NewMassEmailRunner(repo, newMassEmailServiceForTest(repo), nil, nil, nil)
	// 用一个永远抢不到锁的 cache 模拟「别的实例是 leader」。
	runner.leaderLock = &alwaysBusyLeaderLock{}
	runner.tick(context.Background())

	require.Equal(t, domain.MassEmailStatusScheduled, repo.campaigns[1].Status, "非 leader 不应推进定时任务")
	require.False(t, runner.LockHeld())
}

func TestMassEmailRunnerHeartbeatDropsLeadershipOnRenewFailure(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	runner := NewMassEmailRunner(repo, newMassEmailServiceForTest(repo), nil, nil, nil)
	runner.leaderLock = &alwaysBusyLeaderLock{}
	runner.leaderMu.Lock()
	runner.leaderHeld = true
	runner.lockDeadline = time.Now().Add(massEmailGlobalLockTTL)
	runner.newEpochLocked()
	runner.leaderMu.Unlock()

	runner.heartbeat(context.Background())

	require.False(t, runner.LockHeld(), "续期失败必须立即放弃 leadership，后续不再发信")
}

func TestMassEmailRunnerStartStopIsIdempotent(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	runner := NewMassEmailRunner(repo, newMassEmailServiceForTest(repo), nil, nil, nil)

	runner.Start()
	runner.Start()
	runner.Stop()
	runner.Stop()
}

// alwaysBusyLeaderLock 模拟"锁一直被别的实例持有 / 续期失败"。
type alwaysBusyLeaderLock struct{}

func (alwaysBusyLeaderLock) TryAcquireLeaderLock(context.Context, string, string, time.Duration) (bool, error) {
	return false, nil
}

func (alwaysBusyLeaderLock) ReleaseLeaderLock(context.Context, string, string) error { return nil }

func (alwaysBusyLeaderLock) RenewLeaderLock(context.Context, string, string, time.Duration) (bool, error) {
	return false, nil
}

// ---------------------------------------------------------------------------
// Create 与 worker 的竞态（lead #42 C）
// ---------------------------------------------------------------------------

func TestCreateMaterialisesRecipientsBeforeLeavingDraft(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.users = []domain.MassEmailRecipientCandidate{
		{Email: "a@example.com", Source: domain.MassEmailSourceUser},
		{Email: "b@example.com", Source: domain.MassEmailSourceUser},
	}
	svc := newMassEmailServiceForTest(repo)

	campaign, err := svc.Create(context.Background(), 1, MassEmailCreateInput{
		Subject:         "重要通知",
		BodyHTML:        "<p>hi {{name}}</p>",
		RecipientFilter: domain.MassEmailRecipientFilter{IncludeAllUsers: true},
	})
	require.NoError(t, err)

	require.Equal(t, domain.MassEmailStatusDraft, campaign.Status)
	require.Len(t, repo.recipients[campaign.ID], 2, "收件人必须在创建阶段就物化好")
	require.Equal(t, 2, campaign.TotalCount)

	// worker 只有在收件人已经存在时才可能看到 sending：这里 draft 状态下不发送。
	runner := newMassEmailRunnerForTest(repo)
	runner.tick(context.Background())
	require.Equal(t, domain.MassEmailStatusDraft, repo.campaigns[campaign.ID].Status)
}

func TestCreateRollsBackWhenMaterialisationFails(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.users = []domain.MassEmailRecipientCandidate{{Email: "a@example.com", Source: domain.MassEmailSourceUser}}
	repo.failReplace = errors.New("insert failed")
	svc := newMassEmailServiceForTest(repo)

	_, err := svc.Create(context.Background(), 1, MassEmailCreateInput{
		Subject:         "重要通知",
		BodyHTML:        "<p>hi</p>",
		RecipientFilter: domain.MassEmailRecipientFilter{IncludeAllUsers: true},
	})
	require.Error(t, err)
	require.Empty(t, repo.campaigns, "物化失败必须删除半成品任务，不能留下幽灵草稿")
}

// ---------------------------------------------------------------------------
// Update 的 CAS 与"先校验后写"（lead #42 C）
// ---------------------------------------------------------------------------

func TestUpdateRejectsWithoutWritingWhenNoRecipients(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.campaigns[1] = &domain.MassEmailCampaign{
		ID:       1,
		Subject:  "旧主题",
		BodyHTML: "<p>old</p>",
		Status:   domain.MassEmailStatusDraft,
	}
	repo.users = nil // 解析出 0 个收件人
	svc := newMassEmailServiceForTest(repo)

	_, err := svc.Update(context.Background(), 1, MassEmailUpdateInput{
		Subject:         "新主题",
		BodyHTML:        "<p>new</p>",
		RecipientFilter: domain.MassEmailRecipientFilter{IncludeAllUsers: true},
	})
	require.ErrorIs(t, err, domain.ErrMassEmailNoRecipients)
	require.Equal(t, "旧主题", repo.campaigns[1].Subject, "校验失败不能留下部分更新")
	require.Equal(t, 0, repo.replaced, "校验失败不应重建收件人")
}

func TestUpdateReturnsConflictWhenCampaignAdvanced(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.campaigns[1] = &domain.MassEmailCampaign{
		ID:      1,
		Subject: "旧主题",
		Status:  domain.MassEmailStatusDraft,
	}
	repo.users = []domain.MassEmailRecipientCandidate{{Email: "a@example.com", Source: domain.MassEmailSourceUser}}
	svc := newMassEmailServiceForTest(repo)

	// 模拟：读取之后、写入之前，worker 把任务推进到了 sending。
	repo.campaigns[1].Status = domain.MassEmailStatusSending

	_, err := svc.Update(context.Background(), 1, MassEmailUpdateInput{
		Subject:         "新主题",
		BodyHTML:        "<p>new</p>",
		RecipientFilter: domain.MassEmailRecipientFilter{IncludeAllUsers: true},
	})
	// Update 会先读状态，sending 时直接 409。
	require.ErrorIs(t, err, domain.ErrMassEmailSending)
}

func TestUpdateAppliesContentAndRecipientsTogether(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.campaigns[1] = &domain.MassEmailCampaign{
		ID:       1,
		Subject:  "旧主题",
		BodyHTML: "<p>old</p>",
		Status:   domain.MassEmailStatusDraft,
	}
	repo.recipients[1] = []*domain.MassEmailRecipient{
		{ID: 99, CampaignID: 1, Email: "stale@example.com", Status: domain.MassEmailRecipientStatusPending},
	}
	repo.users = []domain.MassEmailRecipientCandidate{{Email: "fresh@example.com", Source: domain.MassEmailSourceUser}}
	svc := newMassEmailServiceForTest(repo)

	updated, err := svc.Update(context.Background(), 1, MassEmailUpdateInput{
		Subject:         "新主题",
		BodyHTML:        "<p>new</p>",
		RecipientFilter: domain.MassEmailRecipientFilter{IncludeAllUsers: true},
	})
	require.NoError(t, err)
	require.Equal(t, "新主题", updated.Subject)
	require.Len(t, repo.recipients[1], 1)
	require.Equal(t, "fresh@example.com", repo.recipients[1][0].Email, "旧收件人必须被替换")
}

// ---------------------------------------------------------------------------
// 主题纯文本渲染（lead #42 E）
// ---------------------------------------------------------------------------

func TestRenderMassEmailTemplatePlainDoesNotEscape(t *testing.T) {
	t.Parallel()

	vars := map[string]string{"name": "A&B <Corp>"}
	require.Equal(t, "主题 A&B <Corp>", RenderMassEmailTemplatePlain("主题 {{name}}", vars))
	// 正文仍然转义
	require.Equal(t, "主题 A&amp;B &lt;Corp&gt;", RenderMassEmailTemplate("主题 {{name}}", vars))
}

// ---------------------------------------------------------------------------
// #48：leadership epoch / 锁过期 / 协调后端一致性 / 清理 ctx
// ---------------------------------------------------------------------------

// erroringLeaderLock 模拟 Redis 故障（不是"被占用"，而是不可用）。
type erroringLeaderLock struct{}

func (erroringLeaderLock) TryAcquireLeaderLock(context.Context, string, string, time.Duration) (bool, error) {
	return false, errors.New("redis down")
}

func (erroringLeaderLock) ReleaseLeaderLock(context.Context, string, string) error { return nil }

func (erroringLeaderLock) RenewLeaderLock(context.Context, string, string, time.Duration) (bool, error) {
	return false, errors.New("redis down")
}

func TestMassEmailRunnerLockExpiryFailsClosed(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusSending}
	repo.recipients[1] = []*domain.MassEmailRecipient{
		{ID: 11, CampaignID: 1, Email: "a@example.com", Status: domain.MassEmailRecipientStatusPending},
	}

	runner := newMassEmailRunnerForTest(repo)
	require.True(t, runner.LockHeld())

	// 模拟进程停顿超过 TTL、heartbeat 没能续期：本地截止时间已过 → fail-closed。
	runner.leaderMu.Lock()
	runner.lockDeadline = time.Now().Add(-time.Second)
	runner.leaderMu.Unlock()

	require.False(t, runner.LockHeld(), "本地续约截止时间过期后必须视为失锁")
	runner.drain(context.Background(), 1)
	require.Equal(t, 0, repo.lastClaimLimit, "过期锁不能继续领取收件人")
}

func TestMassEmailRunnerRedisErrorStaysStandby(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	past := time.Now().Add(-time.Minute)
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusScheduled, ScheduledAt: &past}

	runner := NewMassEmailRunner(repo, newMassEmailServiceForTest(repo), nil, nil, nil)
	// 协调后端是 Redis 且报错：必须保守停发，不能回落到 DB 咨询锁变成双 leader。
	runner.leaderLock = erroringLeaderLock{}
	runner.tick(context.Background())

	require.False(t, runner.LockHeld())
	require.Equal(t, domain.MassEmailStatusScheduled, repo.campaigns[1].Status, "Redis 故障时不得推进任务")
}

func TestMassEmailRunnerLostLeadershipCancelsEpoch(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	runner := newMassEmailRunnerForTest(repo)
	oldEpoch := runner.epochCtx()
	require.NoError(t, oldEpoch.Err())

	// 续期失败 → 取消 epoch（打断限速等待与在途 SMTP）。
	runner.leaderMu.Lock()
	runner.loseLeadershipLocked()
	runner.leaderMu.Unlock()

	require.Error(t, oldEpoch.Err(), "失锁必须取消当前 epoch")

	// 重新拿到 leadership 时是**新** epoch，旧 epoch 不能复活。
	runner.leaderMu.Lock()
	runner.leaderHeld = true
	runner.lockDeadline = time.Now().Add(massEmailGlobalLockTTL)
	runner.newEpochLocked()
	newEpoch := runner.leaderCtx
	runner.leaderMu.Unlock()

	require.NotSame(t, oldEpoch, newEpoch)
	require.Error(t, oldEpoch.Err(), "旧 epoch 永久取消")
	require.NoError(t, newEpoch.Err())
}

func TestMassEmailRunnerDrainStopsWhenEpochCancelled(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusSending}
	repo.recipients[1] = []*domain.MassEmailRecipient{
		{ID: 11, CampaignID: 1, Email: "a@example.com", Status: domain.MassEmailRecipientStatusPending},
	}

	runner := newMassEmailRunnerForTest(repo)
	epoch := runner.epochCtx()
	runner.leaderMu.Lock()
	runner.leaderCancel() // 模拟等待限速时隙期间失锁/取消
	runner.leaderMu.Unlock()

	runner.drain(epoch, 1)
	require.Equal(t, 0, repo.lastClaimLimit, "epoch 已取消就不应再领取收件人")
}

func TestMassEmailRunnerReleaseRemainingUsesCleanupContext(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	claimed := time.Now()
	repo.campaigns[1] = &domain.MassEmailCampaign{ID: 1, Status: domain.MassEmailStatusSending}
	repo.recipients[1] = []*domain.MassEmailRecipient{
		{ID: 11, CampaignID: 1, Email: "a@example.com", Status: domain.MassEmailRecipientStatusPending, ClaimedAt: &claimed, Attempts: 1},
	}

	// 调用方正处于取消路径（ctx 已取消），释放仍然必须成功。
	newMassEmailRunnerForTest(repo).releaseRemaining(1, []domain.MassEmailRecipient{{ID: 11}})

	require.Nil(t, repo.recipients[1][0].ClaimedAt)
	require.Equal(t, 0, repo.recipients[1][0].Attempts)
}

// ---------------------------------------------------------------------------
// 锁租约计时 / watchdog / ABA（T4 #50 复验要求）
// ---------------------------------------------------------------------------

// recordingLeaderLock 模拟 Redis 锁：只有 owner 匹配才能续期/释放（与 Lua CAS 语义一致）。
type recordingLeaderLock struct {
	mu       sync.Mutex
	owner    string
	acquires int

	acquireDelay time.Duration
	renewDelay   time.Duration
	releaseDelay time.Duration
	failRenew    bool
	released     []string
}

func (l *recordingLeaderLock) TryAcquireLeaderLock(ctx context.Context, _ string, owner string, _ time.Duration) (bool, error) {
	if l.acquireDelay > 0 {
		select {
		case <-time.After(l.acquireDelay):
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.owner != "" {
		return false, nil // 已被别人持有
	}
	l.owner = owner
	l.acquires++
	return true, nil
}

func (l *recordingLeaderLock) RenewLeaderLock(ctx context.Context, _ string, owner string, _ time.Duration) (bool, error) {
	if l.renewDelay > 0 {
		select {
		case <-time.After(l.renewDelay):
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failRenew || l.owner != owner {
		return false, nil
	}
	return true, nil
}

func (l *recordingLeaderLock) ReleaseLeaderLock(_ context.Context, _ string, owner string) error {
	if l.releaseDelay > 0 {
		time.Sleep(l.releaseDelay)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.released = append(l.released, owner)
	if l.owner == owner {
		l.owner = ""
	}
	return nil
}

func (l *recordingLeaderLock) currentOwner() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.owner
}

// expire 模拟 Redis 锁 TTL 到期（fake 本身没有 TTL 概念，测试里显式过期）。
func (l *recordingLeaderLock) expire() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.owner = ""
}

func TestMassEmailRunnerDeadlineUsesPreRPCTime(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	lock := &recordingLeaderLock{acquireDelay: 120 * time.Millisecond}
	runner := NewMassEmailRunner(repo, newMassEmailServiceForTest(repo), nil, lock, nil)

	before := time.Now()
	require.True(t, runner.ensureLeader(context.Background()))
	after := time.Now()

	runner.leaderMu.Lock()
	deadline := runner.lockDeadline
	runner.leaderMu.Unlock()

	// 本地截止必须按「调用前」计时：否则请求/响应延迟会把本地截止推到 Redis 实际过期之后。
	require.LessOrEqual(t, deadline.Sub(before), massEmailGlobalLockTTL+20*time.Millisecond)
	require.True(t, deadline.Before(after.Add(massEmailGlobalLockTTL)), "不能用 RPC 返回后的时间计时")
}

func TestMassEmailRunnerWatchdogCancelsEpochAtDeadline(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	lock := &recordingLeaderLock{}
	runner := NewMassEmailRunner(repo, newMassEmailServiceForTest(repo), nil, lock, nil)
	require.True(t, runner.ensureLeader(context.Background()))

	epoch := runner.epochCtx()
	require.NoError(t, epoch.Err())

	// 模拟"续期没来得及发生"：本地截止时间提前到 40ms 后，watchdog 必须主动取消 epoch，
	// 只有 LockHeld()=false 是无法打断在途 SMTP 的。
	runner.leaderMu.Lock()
	runner.lockDeadline = time.Now().Add(40 * time.Millisecond)
	runner.armDeadlineLocked()
	runner.leaderMu.Unlock()

	require.Eventually(t, func() bool {
		return epoch.Err() != nil && !runner.LockHeld()
	}, 2*time.Second, 10*time.Millisecond, "本地截止到期必须取消 epoch（打断在途发送）")
}

func TestMassEmailRunnerHeartbeatDoesNotBlockLockHeld(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	lock := &recordingLeaderLock{renewDelay: 300 * time.Millisecond}
	runner := NewMassEmailRunner(repo, newMassEmailServiceForTest(repo), nil, lock, nil)
	require.True(t, runner.ensureLeader(context.Background()))

	done := make(chan struct{})
	go func() {
		runner.heartbeat(context.Background())
		close(done)
	}()

	time.Sleep(30 * time.Millisecond) // 让 heartbeat 进入 RPC
	start := time.Now()
	held := runner.LockHeld()
	elapsed := time.Since(start)

	require.True(t, held)
	require.Less(t, elapsed, 100*time.Millisecond, "LockHeld 不能被心跳的网络调用阻塞")
	<-done
}

func TestMassEmailRunnerOwnerTokenIsPerAcquisition(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	lock := &recordingLeaderLock{}
	runner := NewMassEmailRunner(repo, newMassEmailServiceForTest(repo), nil, lock, nil)

	require.True(t, runner.ensureLeader(context.Background()))
	runner.leaderMu.Lock()
	firstOwner := runner.leaderOwner
	runner.leaderMu.Unlock()

	// 放弃 leadership 后再获取，必须是新的 owner token。
	runner.leaderMu.Lock()
	runner.loseLeadershipLocked()
	runner.leaderMu.Unlock()
	// 真实 Redis 里旧锁要等 TTL 到期（或异步 Release 完成）才会消失，fake 没有 TTL，
	// 这里显式过期，模拟"锁已可用"的前置状态。
	lock.expire()

	require.True(t, runner.ensureLeader(context.Background()))
	runner.leaderMu.Lock()
	secondOwner := runner.leaderOwner
	runner.leaderMu.Unlock()

	require.NotEmpty(t, firstOwner)
	require.NotEmpty(t, secondOwner)
	require.NotEqual(t, firstOwner, secondOwner, "每次获取锁必须使用独立 owner token")
}

func TestMassEmailRunnerLateReleaseDoesNotDeleteNewLock(t *testing.T) {
	t.Parallel()

	repo := newFakeMassEmailRepo()
	// 旧 epoch 的 Release 延迟 150ms 才真正执行。
	lock := &recordingLeaderLock{releaseDelay: 150 * time.Millisecond}
	runner := NewMassEmailRunner(repo, newMassEmailServiceForTest(repo), nil, lock, nil)

	require.True(t, runner.ensureLeader(context.Background()))
	runner.leaderMu.Lock()
	oldOwner := runner.leaderOwner
	runner.loseLeadershipLocked() // 触发异步 Release（旧 owner）
	runner.leaderMu.Unlock()

	// 旧锁 TTL 到期（fake 显式过期）后，新 epoch 在旧 Release 落地之前拿到锁。
	lock.expire()
	require.True(t, runner.ensureLeader(context.Background()))
	newOwner := lock.currentOwner()
	require.NotEmpty(t, newOwner)
	require.NotEqual(t, oldOwner, newOwner)

	// 等旧 Release 执行完：它只能删自己的 token，不能把新锁删掉（ABA）。
	require.Eventually(t, func() bool {
		lock.mu.Lock()
		defer lock.mu.Unlock()
		return len(lock.released) >= 1
	}, 2*time.Second, 10*time.Millisecond)

	time.Sleep(200 * time.Millisecond)
	require.Equal(t, newOwner, lock.currentOwner(), "旧 epoch 的延迟释放不能删除新 epoch 的锁")
}
