package service

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	// massEmailRunnerTickInterval 调度/驱动轮询间隔。
	massEmailRunnerTickInterval = 5 * time.Second
	// massEmailGlobalLockKey 全局唯一 runner 锁：只有持锁实例会发送邮件。
	massEmailGlobalLockKey = "mass_email:runner"
	// massEmailGlobalLockTTL 全局锁租期；续期周期为其 1/3。
	massEmailGlobalLockTTL = 30 * time.Second
	// massEmailGlobalLockRenewInterval 续期周期（TTL/3）。
	massEmailGlobalLockRenewInterval = 10 * time.Second
	// massEmailSendTimeout 单封邮件的 SMTP 超时。
	massEmailSendTimeout = 30 * time.Second
	// massEmailLockRPCTimeout 获取/续期锁的 RPC 超时（避免 heartbeat 卡死）。
	massEmailLockRPCTimeout = 5 * time.Second
	// massEmailCleanupTimeout 停止/关机时释放 claim 的清理超时（用独立 ctx，不受已取消 ctx 影响）。
	massEmailCleanupTimeout = 10 * time.Second
	// massEmailMaxBatch 单批最大领取数：必须保证「批量 × 单封超时 < 领取租约(5min)」，
	// 8 × 30s = 240s < 300s，留出 60s 余量。
	massEmailMaxBatch = 8
	// massEmailMinBatch 单批最小领取数。
	massEmailMinBatch = 3
)

// massEmailRateLimiter 是进程内**跨任务共享**的限速器。
//
// Resend 的 2 封/秒是账号级（全局）限制，所以多个 campaign 并发发送时必须共用
// 同一个限速器，不能每个任务各跑一个。
type massEmailRateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

func newMassEmailRateLimiter(perSecond int) *massEmailRateLimiter {
	limiter := &massEmailRateLimiter{}
	limiter.SetRate(perSecond)
	return limiter
}

// SetRate 更新速率（封/秒）。
func (l *massEmailRateLimiter) SetRate(perSecond int) {
	if perSecond <= 0 {
		perSecond = DefaultMassEmailRateLimitPerSecond
	}
	if perSecond > MaxMassEmailRateLimitPerSecond {
		perSecond = MaxMassEmailRateLimitPerSecond
	}
	l.mu.Lock()
	l.interval = time.Second / time.Duration(perSecond)
	l.mu.Unlock()
}

// Interval 返回当前速率对应的发送间隔（测试用）。
func (l *massEmailRateLimiter) Interval() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.interval
}

// Wait 阻塞到下一个发送时隙。ctx 取消时立即返回错误。
func (l *massEmailRateLimiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	wait := l.next.Sub(now)
	l.next = l.next.Add(l.interval)
	l.mu.Unlock()

	if wait <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// MassEmailRunner 邮件群发后台驱动。
//
// 并发模型（lead #42 A / #48）：
//   - **全局单例**：所有实例竞争同一个 Redis 锁 \`mass_email:runner\`，只有持锁实例发送；
//   - 持锁期间每 10s（TTL/3）续期，并在本地记录"成功续约截止时间"，到期即 fail-closed；
//   - **每个 leadership 对应一个可取消的 epoch context**：续期失败/锁过期会取消它，
//     从而打断正在等待限速时隙的 goroutine 和正在进行的 SMTP 会话；旧 epoch 不会因为
//     重新拿到锁而复活（新 leadership 一定是新 epoch）。
//   - 只使用 LeaderLockCache 这一个协调后端，Redis 报错时**保守停发**，绝不回落到 DB 咨询锁
//     （否则 Redis 实例与 DB-fallback 实例可能同时成为 leader）。
//   - 进程内所有 campaign 共用一个限速器，保证总发送量不超过设置速率（Resend 账号级 2/s）。
type MassEmailRunner struct {
	repo       MassEmailRepository
	service    *MassEmailService
	email      *EmailService
	leaderLock LeaderLockCache
	db         *sql.DB
	instanceID string

	limiter *massEmailRateLimiter

	leaderMu      sync.Mutex
	leaderHeld    bool
	leaderEpoch   uint64
	lockDeadline  time.Time
	leaderCtx     context.Context
	leaderCancel  context.CancelFunc
	deadlineTimer *time.Timer
	// leaderOwner 是**本次**获取锁的 owner token（每次获取都不同）。
	// 用进程级固定 owner 会出现 ABA：旧 epoch 的异步 Release 延迟执行时
	// 可能把新 epoch 刚拿到的锁删掉。
	leaderOwner string
	// lockRPCTimeout 获取/续期锁的超时（测试可调小）。
	lockRPCTimeout time.Duration

	mu      sync.Mutex
	running map[int64]struct{}
	rootCtx context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	startOnce sync.Once
	stopOnce  sync.Once
}

// NewMassEmailRunner 创建后台驱动（不会自动启动）。
func NewMassEmailRunner(
	repo MassEmailRepository,
	svc *MassEmailService,
	email *EmailService,
	leaderLock LeaderLockCache,
	db *sql.DB,
) *MassEmailRunner {
	return &MassEmailRunner{
		repo:           repo,
		service:        svc,
		email:          email,
		leaderLock:     leaderLock,
		db:             db,
		instanceID:     NewMassEmailTrackingToken(),
		limiter:        newMassEmailRateLimiter(DefaultMassEmailRateLimitPerSecond),
		lockRPCTimeout: massEmailLockRPCTimeout,
		running:        make(map[int64]struct{}),
	}
}

// Start 启动后台驱动（幂等）。
func (r *MassEmailRunner) Start() {
	if r == nil {
		return
	}
	r.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		r.rootCtx = ctx
		r.cancel = cancel

		r.wg.Add(2)
		go func() {
			defer r.wg.Done()
			ticker := time.NewTicker(massEmailRunnerTickInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					r.tick(ctx)
				}
			}
		}()
		go func() {
			defer r.wg.Done()
			ticker := time.NewTicker(massEmailGlobalLockRenewInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					r.heartbeat(ctx)
				}
			}
		}()
		logger.LegacyPrintf("service.mass_email", "[MassEmail] runner started, tick=%s lockTTL=%s", massEmailRunnerTickInterval, massEmailGlobalLockTTL)
	})
}

// Stop 停止后台驱动并等待在途协程退出（幂等）。
func (r *MassEmailRunner) Stop() {
	if r == nil {
		return
	}
	r.stopOnce.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}
		r.leaderMu.Lock()
		if r.leaderCancel != nil {
			r.leaderCancel()
		}
		if r.deadlineTimer != nil {
			r.deadlineTimer.Stop()
			r.deadlineTimer = nil
		}
		held := r.leaderHeld
		owner := r.leaderOwner
		r.leaderHeld = false
		r.leaderMu.Unlock()
		if held && owner != "" && r.leaderLock != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), massEmailCleanupTimeout)
			_ = r.leaderLock.ReleaseLeaderLock(cleanupCtx, massEmailGlobalLockKey, owner)
			cancel()
		}
		r.wg.Wait()
	})
}

// LockHeld 是否**仍然有效**地持有全局 runner 锁（本地截止时间到期即 fail-closed）。
func (r *MassEmailRunner) LockHeld() bool {
	if r == nil {
		return false
	}
	r.leaderMu.Lock()
	defer r.leaderMu.Unlock()
	return r.leaderHeld && time.Now().Before(r.lockDeadline)
}

// epochCtx 返回当前 leadership 对应的 context（没有则返回一个已取消的 ctx）。
func (r *MassEmailRunner) epochCtx() context.Context {
	r.leaderMu.Lock()
	defer r.leaderMu.Unlock()
	if r.leaderCtx == nil {
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		return cancelled
	}
	return r.leaderCtx
}

// newEpochLocked 开启新的 leadership epoch（调用方需持有 leaderMu）。
//
// epoch 版本号单调递增：网络调用返回后用版本号校验，过期结果直接丢弃，
// 防止"旧 epoch 的续期结果"把已经丢失的 leadership 又写回来。
func (r *MassEmailRunner) newEpochLocked() {
	if r.leaderCancel != nil {
		r.leaderCancel()
	}
	parent := r.rootCtx
	if parent == nil {
		parent = context.Background()
	}
	epochCtx, cancel := context.WithCancel(parent)
	r.leaderCtx = epochCtx
	r.leaderCancel = cancel
	r.leaderEpoch++
	r.armDeadlineLocked()
}

// armDeadlineLocked 按本地截止时间安排一个 watchdog（调用方需持有 leaderMu）。
//
// 仅让 LockHeld() 返回 false 是不够的：它无法打断正在等待限速时隙或在途 SMTP 的 goroutine，
// 只有取消 epoch context 才能真正停发。所以这里用定时器主动取消 epoch。
func (r *MassEmailRunner) armDeadlineLocked() {
	if r.deadlineTimer != nil {
		r.deadlineTimer.Stop()
		r.deadlineTimer = nil
	}
	if !r.leaderHeld || r.lockDeadline.IsZero() {
		return
	}
	epoch := r.leaderEpoch
	delay := time.Until(r.lockDeadline)
	if delay < 0 {
		delay = 0
	}
	r.deadlineTimer = time.AfterFunc(delay, func() {
		r.leaderMu.Lock()
		defer r.leaderMu.Unlock()
		if r.leaderEpoch != epoch {
			return // 已经是新的 leadership，旧 watchdog 不生效
		}
		if time.Now().Before(r.lockDeadline) {
			r.armDeadlineLocked() // 续期成功过，重新排期
			return
		}
		logger.LegacyPrintf("service.mass_email", "[MassEmail] lock deadline reached, cancelling epoch")
		r.loseLeadershipLocked()
	})
}

// loseLeadershipLocked 放弃 leadership 并取消当前 epoch（调用方需持有 leaderMu）。
//
// 释放 Redis 锁的网络调用放到 goroutine 里做，避免持有 leaderMu 时阻塞
// LockHeld()/heartbeat（即使失败也无所谓：TTL 会兜底）。
func (r *MassEmailRunner) loseLeadershipLocked() {
	wasHeld := r.leaderHeld
	owner := r.leaderOwner
	r.leaderOwner = ""
	r.leaderHeld = false
	r.lockDeadline = time.Time{}
	if r.deadlineTimer != nil {
		r.deadlineTimer.Stop()
		r.deadlineTimer = nil
	}
	if r.leaderCancel != nil {
		r.leaderCancel()
		r.leaderCtx = nil
		r.leaderCancel = nil
	}
	if !wasHeld || owner == "" || r.leaderLock == nil {
		return
	}
	key := massEmailGlobalLockKey
	lock := r.leaderLock
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), massEmailCleanupTimeout)
		defer cancel()
		_ = lock.ReleaseLeaderLock(ctx, key, owner)
	}()
}

// ensureLeader 尝试成为唯一 runner。已持锁且未过期直接返回 true。
//
// 只用 LeaderLockCache（Redis）；出错时保守返回 false，**不**回落到 DB 咨询锁。
// 网络调用在锁外进行并带超时，本地截止时间按**调用前**的时间计算（保守，避免
// 请求/响应延迟把本地截止推到 Redis 实际过期之后）。
func (r *MassEmailRunner) ensureLeader(ctx context.Context) bool {
	r.leaderMu.Lock()
	if r.leaderHeld && time.Now().Before(r.lockDeadline) {
		r.leaderMu.Unlock()
		return true
	}
	if r.leaderHeld {
		r.loseLeadershipLocked()
	}
	if r.leaderLock == nil {
		// 单实例 / 单元测试：没有分布式协调后端，退化为本地模式。
		r.leaderHeld = true
		r.lockDeadline = time.Now().Add(massEmailGlobalLockTTL)
		r.newEpochLocked()
		r.leaderMu.Unlock()
		return true
	}
	epoch := r.leaderEpoch
	// owner 是纯随机的一次性 token：**不修改** epoch（epoch 只在 newEpochLocked 里递增），
	// 否则 RPC 返回后的版本校验会永远失败，导致永远拿不到 leadership。
	owner := r.newLeaderOwner()
	r.leaderMu.Unlock()

	start := time.Now()
	rpcCtx, cancel := context.WithTimeout(ctx, r.lockRPCTimeout)
	acquired, err := r.leaderLock.TryAcquireLeaderLock(rpcCtx, massEmailGlobalLockKey, owner, massEmailGlobalLockTTL)
	cancel()

	r.leaderMu.Lock()
	defer r.leaderMu.Unlock()
	if r.leaderEpoch != epoch {
		// 期间别的 goroutine 已经改变了 leadership，丢弃这次结果。
		return r.leaderHeld && time.Now().Before(r.lockDeadline)
	}
	if err != nil {
		logger.LegacyPrintf("service.mass_email", "[MassEmail] acquire runner lock failed, stay standby: %v", err)
		return false
	}
	if !acquired {
		return false
	}
	r.leaderHeld = true
	r.leaderOwner = owner
	r.lockDeadline = start.Add(massEmailGlobalLockTTL)
	r.newEpochLocked()
	logger.LegacyPrintf("service.mass_email", "[MassEmail] became runner leader")
	return true
}

// newLeaderOwner 生成一次性 owner token。
//
// 每次成功获取锁都换一个：旧的 epoch 异步 Release 时只会删除自己那个 token 的锁，
// 不会误删新 epoch 刚拿到的锁（ABA）。该函数不改任何状态，可在锁外调用。
func (r *MassEmailRunner) newLeaderOwner() string {
	return r.instanceID + ":" + NewMassEmailTrackingToken()
}

// heartbeat 续期全局锁；失败或 owner 不符立即放弃 leadership 并取消 epoch。
//
// 续期 RPC 在锁外进行（带超时），返回后再用 epoch 版本号校验，避免阻塞 LockHeld()。
func (r *MassEmailRunner) heartbeat(ctx context.Context) {
	if r == nil {
		return
	}
	r.leaderMu.Lock()
	if !r.leaderHeld {
		r.leaderMu.Unlock()
		return
	}
	if r.leaderLock == nil {
		// 本地模式：只续本地截止时间。
		r.lockDeadline = time.Now().Add(massEmailGlobalLockTTL)
		r.armDeadlineLocked()
		r.leaderMu.Unlock()
		return
	}
	epoch := r.leaderEpoch
	owner := r.leaderOwner
	r.leaderMu.Unlock()

	start := time.Now()
	rpcCtx, cancel := context.WithTimeout(ctx, r.lockRPCTimeout)
	renewed, err := r.leaderLock.RenewLeaderLock(rpcCtx, massEmailGlobalLockKey, owner, massEmailGlobalLockTTL)
	cancel()

	r.leaderMu.Lock()
	defer r.leaderMu.Unlock()
	if !r.leaderHeld || r.leaderEpoch != epoch {
		return // 结果已过期
	}
	if err != nil || !renewed {
		logger.LegacyPrintf("service.mass_email", "[MassEmail] lost runner leadership (err=%v renewed=%v), cancelling epoch", err, renewed)
		r.loseLeadershipLocked()
		return
	}
	r.lockDeadline = start.Add(massEmailGlobalLockTTL)
	r.armDeadlineLocked()
}

// tick 推进定时任务并驱动发送（只有 leader 会真正发送）。
func (r *MassEmailRunner) tick(ctx context.Context) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.LegacyPrintf("service.mass_email", "[MassEmail] tick panic: %v", recovered)
		}
	}()

	if !r.ensureLeader(ctx) {
		return // 其它实例是 leader，或协调后端不可用
	}
	epoch := r.epochCtx()
	if epoch.Err() != nil {
		return
	}
	r.limiter.SetRate(r.service.RateLimitPerSecond(epoch))

	now := time.Now()
	due, err := r.repo.ListDueScheduledCampaigns(epoch, now, 20)
	if err != nil {
		logger.LegacyPrintf("service.mass_email", "[MassEmail] list due failed: %v", err)
	}
	for _, campaign := range due {
		ok, transitionErr := r.repo.TransitionCampaignStatus(
			epoch, campaign.ID,
			[]string{domain.MassEmailStatusScheduled},
			domain.MassEmailStatusSending, now,
		)
		if transitionErr != nil {
			logger.LegacyPrintf("service.mass_email", "[MassEmail] promote failed id=%d: %v", campaign.ID, transitionErr)
			continue
		}
		if ok {
			logger.LegacyPrintf("service.mass_email", "[MassEmail] scheduled campaign %d promoted to sending", campaign.ID)
		}
	}

	sending, _, err := r.repo.ListCampaigns(epoch, pagination.PaginationParams{Page: 1, PageSize: 50}, domain.MassEmailCampaignListFilters{
		Statuses: []string{domain.MassEmailStatusSending},
	})
	if err != nil {
		logger.LegacyPrintf("service.mass_email", "[MassEmail] list sending failed: %v", err)
		return
	}
	for i := range sending {
		r.ensureWorker(epoch, sending[i].ID)
	}
}

// ensureWorker 为任务启动一个本进程内的发送协程（同一任务不重复启动）。
func (r *MassEmailRunner) ensureWorker(epoch context.Context, campaignID int64) {
	r.mu.Lock()
	if _, exists := r.running[campaignID]; exists {
		r.mu.Unlock()
		return
	}
	r.running[campaignID] = struct{}{}
	r.mu.Unlock()

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			r.mu.Lock()
			delete(r.running, campaignID)
			r.mu.Unlock()
			if recovered := recover(); recovered != nil {
				logger.LegacyPrintf("service.mass_email", "[MassEmail] worker panic campaign=%d: %v", campaignID, recovered)
			}
		}()
		r.drain(epoch, campaignID)
	}()
}

// drain 发送一个任务的待发收件人，直到队列空、任务被取消或失去 leadership。
//
// epoch 是本次 leadership 专属的 context：续期失败 / 锁过期 / 进程停止都会取消它，
// 从而打断限速等待与在途 SMTP，旧 epoch 不会复活。
func (r *MassEmailRunner) drain(epoch context.Context, campaignID int64) {
	batchSize := massEmailBatchSize(r.service.RateLimitPerSecond(epoch))
	siteName := r.service.siteName(epoch)
	enterpriseNames := make(map[int64]string)

	for {
		if epoch.Err() != nil || !r.LockHeld() {
			return
		}

		campaign, err := r.repo.GetCampaign(epoch, campaignID)
		if err != nil || campaign.Status != domain.MassEmailStatusSending {
			return
		}

		recipients, claimErr := r.repo.ClaimPendingRecipients(
			epoch, campaignID, batchSize, domain.MassEmailClaimLease, time.Now(),
		)
		if claimErr != nil {
			logger.LegacyPrintf("service.mass_email", "[MassEmail] claim failed campaign=%d: %v", campaignID, claimErr)
			return
		}
		if len(recipients) == 0 {
			r.finalize(campaignID)
			return
		}

		config, configErr := r.email.GetSMTPConfig(epoch)
		if configErr != nil {
			for i := range recipients {
				_ = r.repo.MarkRecipientFailed(epoch, campaignID, recipients[i].ID, "SMTP 未配置: "+configErr.Error(), time.Now())
			}
			_ = r.repo.UpdateCampaignError(epoch, campaignID, "SMTP 未配置，无法发送")
			_, _ = r.repo.RecountCampaign(epoch, campaignID)
			r.finalize(campaignID)
			return
		}

		pixelBase := r.service.frontendURL(epoch)

		for i := range recipients {
			if epoch.Err() != nil || !r.LockHeld() {
				r.releaseRemaining(campaignID, recipients[i:])
				return
			}

			// 共享限速器：所有任务共用，保证账号级总速率。
			// 注意：50 个任务共享 2/s 时这里可能等 25 秒，所以等待之后必须重新校验。
			if err := r.limiter.Wait(epoch); err != nil {
				r.releaseRemaining(campaignID, recipients[i:])
				return
			}
			// 等待期间可能失去锁 / 任务被取消 → 发送前再校验一次。
			if epoch.Err() != nil || !r.LockHeld() {
				r.releaseRemaining(campaignID, recipients[i:])
				return
			}
			latest, loadErr := r.repo.GetCampaign(epoch, campaignID)
			if loadErr != nil || latest.Status != domain.MassEmailStatusSending {
				r.releaseRemaining(campaignID, recipients[i:])
				return
			}

			recipient := recipients[i]
			if recipient.EnterpriseID != nil {
				key := *recipient.EnterpriseID
				if _, ok := enterpriseNames[key]; !ok {
					if names, nameErr := r.repo.MassEmailEnterpriseNames(epoch, []int64{key}); nameErr == nil {
						if value, exists := names[key]; exists {
							enterpriseNames[key] = value
						}
					}
				}
			}

			sendErr := r.sendOne(epoch, config, campaign, &recipient, siteName, enterpriseNames, pixelBase)

			var markErr error
			if sendErr != nil {
				markErr = r.repo.MarkRecipientFailed(epoch, campaignID, recipient.ID, sendErr.Error(), time.Now())
			} else {
				markErr = r.repo.MarkRecipientSent(epoch, campaignID, recipient.ID, time.Now())
			}
			if markErr != nil {
				// 落库失败：不能再继续（否则租约到期后会被重复领取重发）。
				logger.LegacyPrintf("service.mass_email", "[MassEmail] mark recipient failed campaign=%d recipient=%d: %v", campaignID, recipient.ID, markErr)
				_ = r.repo.UpdateCampaignError(epoch, campaignID, "更新收件人状态失败，已停止发送")
				r.releaseRemaining(campaignID, recipients[i+1:])
				return
			}
		}

		if _, err := r.repo.RecountCampaign(epoch, campaignID); err != nil {
			logger.LegacyPrintf("service.mass_email", "[MassEmail] recount failed campaign=%d: %v", campaignID, err)
		}
	}
}

// massEmailBatchSize 单批领取数量 = 速率×10 秒，并夹在 [3, 8]。
//
// 上限 8 是为了满足 lead #42 B 的约束：8 × 30s(单封超时) = 240s < 5min 领取租约，
// 保证「已领取但还没发」的收件人不会因为租约过期被别的实例重复领取。
func massEmailBatchSize(ratePerSecond int) int {
	if ratePerSecond <= 0 {
		ratePerSecond = DefaultMassEmailRateLimitPerSecond
	}
	size := ratePerSecond * 10
	if size < massEmailMinBatch {
		size = massEmailMinBatch
	}
	if size > massEmailMaxBatch {
		size = massEmailMaxBatch
	}
	return size
}

// releaseRemaining 把已领取但还没发送的收件人放回队列（清空 claimed_at、attempts 减回）。
//
// 用**独立的短超时 ctx**：调用方往往正处在取消/关机路径上，复用已取消的 ctx 会让释放失败，
// 导致这些收件人被 5 分钟租约卡住。
func (r *MassEmailRunner) releaseRemaining(campaignID int64, recipients []domain.MassEmailRecipient) {
	if len(recipients) == 0 {
		return
	}
	ids := make([]int64, 0, len(recipients))
	for i := range recipients {
		ids = append(ids, recipients[i].ID)
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), massEmailCleanupTimeout)
	defer cancel()
	if _, err := r.repo.ReleaseClaimedRecipients(cleanupCtx, campaignID, ids); err != nil {
		logger.LegacyPrintf("service.mass_email", "[MassEmail] release claimed failed campaign=%d: %v", campaignID, err)
	}
}

// finalize 队列发完后收尾：pending=0 时把 sending 推进到 completed / failed。
func (r *MassEmailRunner) finalize(campaignID int64) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), massEmailCleanupTimeout)
	defer cancel()
	stats, err := r.repo.RecountCampaign(cleanupCtx, campaignID)
	if err != nil {
		return
	}
	if stats.Pending > 0 {
		return
	}
	target := domain.MassEmailStatusCompleted
	if stats.Sent == 0 && stats.Failed > 0 {
		target = domain.MassEmailStatusFailed
	}
	if _, err := r.repo.TransitionCampaignStatus(
		cleanupCtx, campaignID, []string{domain.MassEmailStatusSending}, target, time.Now(),
	); err != nil {
		logger.LegacyPrintf("service.mass_email", "[MassEmail] finalize failed campaign=%d: %v", campaignID, err)
	}
}

// sendOne 渲染并发送一封邮件。
func (r *MassEmailRunner) sendOne(
	ctx context.Context,
	config *SMTPConfig,
	campaign *domain.MassEmailCampaign,
	recipient *domain.MassEmailRecipient,
	siteName string,
	enterpriseNames map[int64]string,
	pixelBase string,
) error {
	vars := map[string]string{
		"name":            massEmailRecipientDisplayName(recipient),
		"email":           recipient.Email,
		"enterprise_name": massEmailEnterpriseName(recipient, enterpriseNames),
		"site_name":       siteName,
	}
	// 主题是纯文本头部，不做 HTML 转义；正文保留 HTML 转义。
	subject := RenderMassEmailTemplatePlain(campaign.Subject, vars)
	body := RenderMassEmailTemplate(campaign.BodyHTML, vars)

	if pixelBase != "" && recipient.TrackingToken != "" {
		pixelURL := strings.TrimRight(pixelBase, "/") + "/api/v1/mail/o/" + recipient.TrackingToken + ".gif"
		body = InjectMassEmailTrackingPixel(body, pixelURL)
	}

	sendCtx, cancel := context.WithTimeout(ctx, massEmailSendTimeout)
	defer cancel()
	return r.email.sendEmailWithConfigContext(sendCtx, config, recipient.Email, subject, body)
}

func massEmailRecipientDisplayName(recipient *domain.MassEmailRecipient) string {
	if recipient == nil {
		return ""
	}
	if name := strings.TrimSpace(recipient.Name); name != "" {
		return name
	}
	email := domain.NormalizeEmail(recipient.Email)
	if at := strings.IndexByte(email, '@'); at > 0 {
		return email[:at]
	}
	return email
}

func massEmailEnterpriseName(recipient *domain.MassEmailRecipient, names map[int64]string) string {
	if recipient == nil || recipient.EnterpriseID == nil {
		return ""
	}
	return names[massEmailEnterpriseKey(recipient.EnterpriseID)]
}

func massEmailEnterpriseKey(id *int64) int64 {
	if id == nil {
		return 0
	}
	return *id
}
