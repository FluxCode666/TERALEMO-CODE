//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type videoTaskBillingFake struct {
	mu                 sync.Mutex
	tasks              map[string]*VideoBillingReservation
	settlements        []*VideoReservationSettlement
	settleCalls        int
	settleFailures     int
	releases           []string
	manual             []string
	reschedules        []time.Time
	rescheduleErrors   []string
	rescheduleContexts []error
	claimOwner         string
	claimLimit         int
	claimLease         time.Duration
	claimErr           error
	reserveErr         error
}

func cloneVideoTask(v *VideoBillingReservation) *VideoBillingReservation {
	if v == nil {
		return nil
	}
	copy := *v
	copy.Contract = append(json.RawMessage(nil), v.Contract...)
	copy.Snapshot = append(json.RawMessage(nil), v.Snapshot...)
	copy.EnterpriseDepartmentPathIDs = append([]int64(nil), v.EnterpriseDepartmentPathIDs...)
	return &copy
}
func newVideoTaskBillingFake() *videoTaskBillingFake {
	return &videoTaskBillingFake{tasks: make(map[string]*VideoBillingReservation)}
}
func (f *videoTaskBillingFake) Reserve(_ context.Context, v *VideoBillingReservation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reserveErr != nil {
		return f.reserveErr
	}
	v.State = VideoReservationPreparing
	v.CreatedAt = time.Now()
	f.tasks[v.ID] = cloneVideoTask(v)
	return nil
}
func (f *videoTaskBillingFake) Accept(_ context.Context, id, taskID string, accountID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := f.tasks[id]
	if v == nil {
		return ErrVideoReservationNotFound
	}
	if v.AccountID != accountID {
		return ErrVideoReservationConflict
	}
	v.TaskID = taskID
	v.State = VideoReservationAccepted
	return nil
}
func (f *videoTaskBillingFake) Get(_ context.Context, id string) (*VideoBillingReservation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v := f.tasks[id]; v != nil {
		return cloneVideoTask(v), nil
	}
	return nil, ErrVideoReservationNotFound
}
func (f *videoTaskBillingFake) GetByTask(_ context.Context, protocol, taskID string) (*VideoBillingReservation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range f.tasks {
		if v.Protocol == protocol && v.TaskID == taskID {
			return cloneVideoTask(v), nil
		}
	}
	return nil, ErrVideoReservationNotFound
}
func (f *videoTaskBillingFake) BindFile(_ context.Context, id, fileID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[id].FileID = fileID
	return nil
}
func (f *videoTaskBillingFake) GetByFile(_ context.Context, protocol, fileID string) (*VideoBillingReservation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range f.tasks {
		if v.Protocol == protocol && v.FileID == fileID {
			return cloneVideoTask(v), nil
		}
	}
	return nil, ErrVideoReservationNotFound
}
func (f *videoTaskBillingFake) ClaimDue(_ context.Context, owner string, limit int, lease time.Duration) ([]*VideoBillingReservation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claimOwner, f.claimLimit, f.claimLease = owner, limit, lease
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	var tasks []*VideoBillingReservation
	for _, v := range f.tasks {
		if v.State == VideoReservationPreparing || v.State == VideoReservationAccepted {
			v.LeaseOwner = owner
			v.Attempts++
			tasks = append(tasks, cloneVideoTask(v))
			if len(tasks) == limit {
				break
			}
		}
	}
	return tasks, nil
}
func (f *videoTaskBillingFake) Reschedule(ctx context.Context, id, owner string, next time.Time, detail string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := f.tasks[id]
	if v.State != VideoReservationAccepted && v.State != VideoReservationPreparing {
		return ErrVideoReservationConflict
	}
	if v.LeaseOwner != owner {
		return ErrVideoReservationConflict
	}
	f.reschedules = append(f.reschedules, next)
	f.rescheduleErrors = append(f.rescheduleErrors, detail)
	f.rescheduleContexts = append(f.rescheduleContexts, ctx.Err())
	return nil
}
func (f *videoTaskBillingFake) MarkManual(_ context.Context, id, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[id].State = VideoReservationManual
	f.manual = append(f.manual, reason)
	return nil
}
func (f *videoTaskBillingFake) Settle(_ context.Context, input *VideoReservationSettlement) (*VideoReservationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settleCalls++
	if f.settleFailures > 0 {
		f.settleFailures--
		return nil, errors.New("injected transaction failure")
	}
	v := f.tasks[input.ID]
	if v.State == VideoReservationSettled {
		return &VideoReservationResult{ActualAmount: v.ActualAmount}, nil
	}
	v.State = VideoReservationSettled
	v.ActualAmount = input.Usage.ActualCost
	copy := *input
	usage := *input.Usage
	copy.Usage = &usage
	f.settlements = append(f.settlements, &copy)
	return &VideoReservationResult{Applied: true, ActualAmount: usage.ActualCost}, nil
}
func (f *videoTaskBillingFake) Release(_ context.Context, id, reason string) (*VideoReservationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := f.tasks[id]
	if v.State == VideoReservationReleased || v.State == VideoReservationSettled {
		return &VideoReservationResult{}, nil
	}
	v.State = VideoReservationReleased
	f.releases = append(f.releases, reason)
	return &VideoReservationResult{Applied: true}, nil
}

func videoTaskServiceFixture(t *testing.T) (*OpenAIGatewayService, *videoTaskBillingFake, ReserveVideoTaskInput) {
	t.Helper()
	repo := newVideoTaskBillingFake()
	groupID := int64(73)
	price := .1
	user := &User{ID: 71, Balance: 10, PasswordHash: "do-not-persist-password"}
	key := &APIKey{ID: 72, UserID: user.ID, User: user, Key: "do-not-persist-api-key", GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformVideo, RateMultiplier: 1, VideoPrice720P: &price}}
	account := seedanceTestAccount()
	account.ID = 74
	account.Credentials["api_key"] = "do-not-persist-account-secret"
	svc := &OpenAIGatewayService{}
	svc.SetVideoBillingReservationRepository(repo)
	input := ReserveVideoTaskInput{APIKey: key, Account: account, Endpoint: SeedanceEndpointCreate, Protocol: VideoProtocolSeedance,
		Model: "doubao-seedance", Resolution: "720p", DurationSeconds: 6, MaxDurationSeconds: 6, VideoCount: 1,
		InboundEndpoint: "/api/v3/contents/generations/tasks", UpstreamEndpoint: "/api/v3/contents/generations/tasks"}
	return svc, repo, input
}

func acceptVideoFixture(t *testing.T, svc *OpenAIGatewayService, input ReserveVideoTaskInput) *VideoBillingReservation {
	t.Helper()
	v, err := svc.ReserveVideoTask(context.Background(), input)
	require.NoError(t, err)
	require.NoError(t, svc.AcceptVideoTask(context.Background(), v, "seedance:task-test"))
	return v
}

func TestVideoBillingTasksFrozenPriceAfterMutationAndRestart(t *testing.T) {
	svc, repo, input := videoTaskServiceFixture(t)
	v := acceptVideoFixture(t, svc, input)
	for _, secret := range []string{input.APIKey.Key, input.APIKey.User.PasswordHash, input.Account.Credentials["api_key"].(string)} {
		require.NotContains(t, string(v.Snapshot), secret)
		require.NotContains(t, string(v.Contract), secret)
	}
	input.APIKey.Group.VideoBillingMode = "per_request"
	input.APIKey.Group.VideoPricePerRequest = f64p(2)
	input.APIKey.Group.RateMultiplier = 3
	input.APIKey.Group.VideoPrice720P = f64p(9)
	input.APIKey.GroupID = i64p(999)
	input.APIKey.User.Enterprise = &EnterpriseMemberContext{EnterpriseID: 900, MemberID: 901}
	// A new service has no reference to the authenticated key, group or user.
	restarted := &OpenAIGatewayService{}
	restarted.SetVideoBillingReservationRepository(repo)
	handled, err := restarted.ObserveVideoTaskBilling(context.Background(), v.Protocol, v.TaskID, &OpenAIForwardResult{AsyncTaskStatus: "completed", VideoCount: 1, VideoDurationSeconds: 6}, false)
	require.True(t, handled)
	require.NoError(t, err)
	require.Len(t, repo.settlements, 1)
	log := repo.settlements[0].Usage
	require.InDelta(t, .6, log.ActualCost, 1e-12)
	require.Equal(t, "video", *log.BillingMode)
	require.Equal(t, 1.0, log.RateMultiplier)
	require.Equal(t, int64(73), *log.GroupID)
	require.Nil(t, log.EnterpriseID)
}

func TestVideoBillingTasksEnterprisePayerSnapshotAndFailClosed(t *testing.T) {
	svc, repo, input := videoTaskServiceFixture(t)
	departmentID := int64(802)
	input.APIKey.User.Enterprise = &EnterpriseMemberContext{EnterpriseID: 800, MemberID: 801, DepartmentID: &departmentID, DepartmentPathIDs: []int64{803, 802}}
	v := acceptVideoFixture(t, svc, input)
	require.Equal(t, int64(800), v.EnterpriseID)
	require.Equal(t, int64(801), v.EnterpriseMemberID)
	require.Equal(t, []int64{803, 802}, v.EnterpriseDepartmentPathIDs)
	input.APIKey.User.Enterprise = nil
	_, err := svc.ObserveVideoTaskBilling(context.Background(), v.Protocol, v.TaskID, &OpenAIForwardResult{AsyncTaskStatus: "completed", VideoCount: 1, VideoDurationSeconds: 4}, false)
	require.NoError(t, err)
	require.Equal(t, int64(800), *repo.settlements[0].Usage.EnterpriseID)
	require.Equal(t, int64(802), *repo.settlements[0].Usage.EnterpriseDepartmentID)
	missing := &OpenAIGatewayService{}
	_, err = missing.ReserveVideoTask(context.Background(), input)
	require.ErrorIs(t, err, ErrBillingServiceUnavailable)
	svc, repo, input = videoTaskServiceFixture(t)
	repo.reserveErr = ErrInsufficientBalance
	_, err = svc.ReserveVideoTask(context.Background(), input)
	require.ErrorIs(t, err, ErrInsufficientBalance)
	require.Empty(t, repo.tasks)
}

func TestVideoBillingTasksSmartDurationReservesMaximum(t *testing.T) {
	svc, repo, input := videoTaskServiceFixture(t)
	input.DurationSeconds = -1
	input.MaxDurationSeconds = 30
	v := acceptVideoFixture(t, svc, input)
	require.InDelta(t, 3, v.HoldAmount, 1.1e-8)
	_, err := svc.ObserveVideoTaskBilling(context.Background(), v.Protocol, v.TaskID, &OpenAIForwardResult{AsyncTaskStatus: "completed", VideoCount: 1, VideoDurationSeconds: 8}, false)
	require.NoError(t, err)
	require.InDelta(t, .8, repo.settlements[0].Usage.ActualCost, 1e-12)
}

func TestVideoBillingTasksTerminalObserversAndRetry(t *testing.T) {
	for _, protocol := range []string{VideoProtocolSeedance, VideoProtocolGemini, VideoProtocolMiniMax, "grok"} {
		// All route kinds share this observer. The service accepts either the
		// original task ID or the MiniMax file indirection; route wiring is
		// exercised separately by handler tests.
		for _, observer := range []string{"task-id", "minimax-file"} {
			t.Run(protocol+"/"+observer, func(t *testing.T) {
				svc, repo, input := videoTaskServiceFixture(t)
				input.Protocol = protocol
				if protocol == "grok" {
					input.Account.Platform = PlatformGrok
				}
				v := acceptVideoFixture(t, svc, input)
				repo.settleFailures = 1
				result := &OpenAIForwardResult{AsyncTaskStatus: "completed", VideoCount: 1, VideoDurationSeconds: 6, VideoFileID: "file-test"}
				_, err := svc.ObserveVideoTaskBilling(context.Background(), protocol, v.TaskID, result, false)
				require.Error(t, err)
				require.Empty(t, repo.settlements)
				taskID := v.TaskID
				if observer == "minimax-file" {
					taskID = "minimax-file:file-test"
				}
				handled, err := svc.ObserveVideoTaskBilling(context.Background(), protocol, taskID, result, false)
				require.True(t, handled)
				require.NoError(t, err)
				_, err = svc.ObserveVideoTaskBilling(context.Background(), protocol, taskID, result, false)
				require.NoError(t, err)
				require.Len(t, repo.settlements, 1)
				require.Equal(t, StableGrokVideoBillingRequestID(v.TaskID), repo.settlements[0].Usage.RequestID)
			})
		}
	}
}

func TestVideoBillingTasksFailureCancellationAndNonterminal(t *testing.T) {
	for _, status := range []string{"failed", "cancelled", "canceled", "expired", "running", "queued"} {
		t.Run(status, func(t *testing.T) {
			svc, repo, input := videoTaskServiceFixture(t)
			v := acceptVideoFixture(t, svc, input)
			_, err := svc.ObserveVideoTaskBilling(context.Background(), v.Protocol, v.TaskID, &OpenAIForwardResult{AsyncTaskStatus: status}, false)
			require.NoError(t, err)
			if status == "running" || status == "queued" {
				require.Empty(t, repo.releases)
			} else {
				require.Len(t, repo.releases, 1)
			}
			require.Empty(t, repo.settlements)
		})
	}
	svc, repo, input := videoTaskServiceFixture(t)
	v := acceptVideoFixture(t, svc, input)
	_, err := svc.ObserveVideoTaskBilling(context.Background(), v.Protocol, v.TaskID, &OpenAIForwardResult{}, true)
	require.NoError(t, err)
	require.Len(t, repo.releases, 1)
	handled, err := svc.ObserveVideoTaskBilling(context.Background(), v.Protocol, "legacy-task", &OpenAIForwardResult{}, false)
	require.False(t, handled)
	require.NoError(t, err)
}

func TestVideoBillingTasksActualDurationFramesDynamicAndToken(t *testing.T) {
	for _, test := range []struct {
		name             string
		requested        float64
		frames, returned int
		want             float64
		wantErr          bool
	}{
		{"actual wins", 6, 0, 3, .3, false}, {"requested fallback", 6, 0, 0, .6, false},
		{"frames exact", 289.0 / 24, 289, 0, 289.0 / 240, false}, {"dynamic actual", 0, 0, 7, .7, false},
		{"dynamic missing", 0, 0, 0, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, repo, input := videoTaskServiceFixture(t)
			input.DurationSeconds = test.requested
			input.Frames = test.frames
			input.MaxDurationSeconds = 30
			v := acceptVideoFixture(t, svc, input)
			_, err := svc.ObserveVideoTaskBilling(context.Background(), v.Protocol, v.TaskID, &OpenAIForwardResult{AsyncTaskStatus: "completed", VideoCount: 1, VideoDurationSeconds: test.returned}, false)
			if test.wantErr {
				require.Error(t, err)
				require.Empty(t, repo.settlements)
			} else {
				require.NoError(t, err)
				require.InDelta(t, test.want, repo.settlements[0].Usage.ActualCost, 1e-12)
			}
		})
	}
	svc, repo, input := videoTaskServiceFixture(t)
	v := acceptVideoFixture(t, svc, input)
	contract := VideoPricingContract{Version: VideoPricingContractVersion, Model: "token-model", Resolution: "720p", Mode: BillingModeToken,
		TokenPricing: &ModelPricing{OutputPricePerToken: .000002}, EffectiveMultiplier: 1, ChannelTimeMultiplier: 1, ReasoningMultiplier: 1, ReservedAmount: 1, MaxDurationSeconds: 30, MaxVideoCount: 1}
	encoded, err := json.Marshal(contract)
	require.NoError(t, err)
	repo.tasks[v.ID].Contract = encoded
	_, err = svc.ObserveVideoTaskBilling(context.Background(), v.Protocol, v.TaskID, &OpenAIForwardResult{AsyncTaskStatus: "completed", Usage: OpenAIUsage{OutputTokens: 1000}}, false)
	require.NoError(t, err)
	require.InDelta(t, .002, repo.settlements[0].Usage.ActualCost, 1e-12)
}

type videoBoundAccountFake struct {
	AccountRepository
	account *Account
	err     error
	ids     []int64
	mu      sync.Mutex
}

func (f *videoBoundAccountFake) GetByID(_ context.Context, id int64) (*Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids = append(f.ids, id)
	return f.account, f.err
}

func TestVideoBillingTasksReconcilerUsesOriginalAccountWithoutKey(t *testing.T) {
	svc, repo, input := videoTaskServiceFixture(t)
	v := acceptVideoFixture(t, svc, input)
	accounts := &videoBoundAccountFake{account: input.Account}
	upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(`{"id":"task-test","status":"succeeded","duration":4,"usage":{"completion_tokens":1234}}`)}
	restarted := &OpenAIGatewayService{accountRepo: accounts, httpUpstream: upstream}
	restarted.SetVideoBillingReservationRepository(repo)
	// There is deliberately no apiKeyRepo, userRepo, groupRepo or mutable auth.
	input.APIKey.Key = "deleted"
	input.APIKey.Group = nil
	input.APIKey.User = nil
	require.NoError(t, restarted.ReconcileVideoBillingOnce(context.Background(), "instance-two"))
	require.Equal(t, []int64{v.AccountID}, accounts.ids)
	require.Equal(t, "instance-two", repo.claimOwner)
	require.Equal(t, 4, repo.claimLimit)
	require.Equal(t, 3*time.Minute, repo.claimLease)
	require.Equal(t, http.MethodGet, upstream.request.Method)
	require.Equal(t, "/api/v3/contents/generations/tasks/task-test", upstream.request.URL.Path)
	require.Len(t, repo.settlements, 1)
	require.InDelta(t, .4, repo.settlements[0].Usage.ActualCost, 1e-12)
}

func TestVideoBillingTasksReconcilerBackoffAndManualRetainsHold(t *testing.T) {
	for _, attempt := range []int{0, 2, 10} {
		t.Run(fmt.Sprint(attempt), func(t *testing.T) {
			svc, repo, input := videoTaskServiceFixture(t)
			v := acceptVideoFixture(t, svc, input)
			repo.tasks[v.ID].Attempts = attempt
			svc.accountRepo = &videoBoundAccountFake{err: errors.New("temporarily unavailable")}
			before := time.Now()
			require.NoError(t, svc.ReconcileVideoBillingOnce(context.Background(), "worker"))
			require.Len(t, repo.reschedules, 1)
			delay := repo.reschedules[0].Sub(before)
			wantDelay := map[int]time.Duration{0: 30 * time.Second, 2: 2 * time.Minute, 10: 30 * time.Minute}[attempt]
			require.GreaterOrEqual(t, delay, wantDelay)
			require.Less(t, delay, wantDelay+time.Second)
			require.NotEmpty(t, repo.rescheduleErrors[0])
			require.NoError(t, repo.rescheduleContexts[0])
			require.Empty(t, repo.releases)
			require.Empty(t, repo.manual)
		})
	}
	for _, kind := range []string{"old task", "unknown submission"} {
		t.Run(kind, func(t *testing.T) {
			svc, repo, input := videoTaskServiceFixture(t)
			v := acceptVideoFixture(t, svc, input)
			if kind == "old task" {
				repo.tasks[v.ID].CreatedAt = time.Now().Add(-8 * 24 * time.Hour)
			} else {
				repo.tasks[v.ID].TaskID = ""
				repo.tasks[v.ID].State = VideoReservationPreparing
				repo.tasks[v.ID].CreatedAt = time.Now().Add(-4 * time.Minute)
			}
			require.NoError(t, svc.ReconcileVideoBillingOnce(context.Background(), "worker"))
			require.Len(t, repo.manual, 1)
			require.Empty(t, repo.releases)
			require.Empty(t, repo.settlements)
			require.InDelta(t, .6, repo.tasks[v.ID].HoldAmount, 1.1e-8)
			require.Equal(t, VideoReservationManual, repo.tasks[v.ID].State)
		})
	}
}

func TestVideoBillingTasksRetryUsesLiveDetachedContext(t *testing.T) {
	svc, repo, input := videoTaskServiceFixture(t)
	v := acceptVideoFixture(t, svc, input)
	v.LeaseOwner = "worker"
	repo.tasks[v.ID].LeaseOwner = "worker"
	svc.accountRepo = &videoBoundAccountFake{err: context.DeadlineExceeded}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc.reconcileVideoTask(ctx, "worker", v)
	require.Len(t, repo.reschedules, 1)
	require.NoError(t, repo.rescheduleContexts[0])
	require.Empty(t, repo.releases)
}

func TestSeedanceSubmissionTimingContract(t *testing.T) {
	for _, test := range []struct {
		name, model, extra, prompt            string
		wantDuration, wantFrames, wantReserve int
		wantErr                               bool
	}{
		{"fixed", "doubao-seedance-1-0-pro", `,"duration":6`, "waves", 6, 0, 6, false},
		{"default fixed", "doubao-seedance-1-0-pro", "", "waves", 5, 0, 5, false},
		{"default alias", "ep-deployment", "", "waves", 5, 0, 5, false},
		{"default smart25", "doubao-seedance-2-5-pro", "", "waves", -1, 0, 30, false},
		{"prompt duration", "doubao-seedance-1-0-pro", "", "waves --duration 8", 8, 0, 8, false},
		{"prompt short flag", "doubao-seedance-1-0-pro", "", "waves --dur 7", 7, 0, 7, false},
		{"frames", "doubao-seedance-1-0-pro", `,"frames":289`, "waves", 13, 289, 13, false},
		{"prompt frames", "doubao-seedance-1-0-pro", "", "waves --frames 29", 2, 29, 2, false},
		{"smart20", "doubao-seedance-2-0-pro", `,"duration":-1`, "waves", -1, 0, 15, false},
		{"smart15", "doubao-seedance-1-5-pro", `,"duration":-1`, "waves", -1, 0, 12, false},
		{"fixed15", "doubao-seedance-1.5-pro", `,"duration":4`, "waves", 4, 0, 4, false},
		{"under minimum15", "doubao-seedance-1-5-pro", `,"duration":3`, "waves", 0, 0, 0, true},
		{"smart25", "doubao-seedance-2-5-pro", `,"duration":-1`, "waves", -1, 0, 30, false},
		{"smart alias rejected", "ep-deployment", `,"duration":-1`, "waves", 0, 0, 0, true},
		{"conflicting flags", "doubao-seedance-1-0-pro", `,"duration":6`, "waves --duration 8", 0, 0, 0, true},
		{"unsupported fps", "doubao-seedance-1-0-pro", "", "waves --fps 60", 0, 0, 0, true},
		{"missing weak value", "doubao-seedance-1-0-pro", "", "waves --duration", 0, 0, 0, true},
		{"equals weak value", "doubao-seedance-1-0-pro", "", "waves --duration=8", 0, 0, 0, true},
		{"malformed frames", "doubao-seedance-1-0-pro", "", "waves --frames abc", 0, 0, 0, true},
		{"equals fps", "doubao-seedance-1-0-pro", "", "waves --fps=24", 0, 0, 0, true},
		{"unsupported duration alias", "doubao-seedance-1-0-pro", "", "waves --duration_seconds 8", 0, 0, 0, true},
		{"invalid frames", "doubao-seedance-1-0-pro", `,"frames":30`, "waves", 0, 0, 0, true},
		{"frames wrong model", "doubao-seedance-2-0-pro", `,"frames":289`, "waves", 0, 0, 0, true},
		{"noninteger duration", "doubao-seedance-1-0-pro", `,"duration":6.5`, "waves", 0, 0, 0, true},
		{"over upper bound", "doubao-seedance-2-0-pro", `,"duration":30`, "waves", 0, 0, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			prompt, _ := json.Marshal(test.prompt)
			body := []byte(`{"model":"video","content":[{"type":"text","text":` + string(prompt) + `}]` + test.extra + `}`)
			forwarded, info, err := NormalizeSeedanceSubmission(body, test.model)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantDuration, info.DurationSeconds)
			require.Equal(t, test.wantFrames, info.Frames)
			require.Equal(t, test.wantReserve, SeedanceReservationDuration(info, test.model))
			if test.wantFrames > 0 {
				require.Equal(t, int64(test.wantFrames), gjson.GetBytes(forwarded, "frames").Int())
				require.False(t, gjson.GetBytes(forwarded, "duration").Exists())
				require.InDelta(t, float64(test.wantFrames)/24, info.DurationSecondsExact, 1e-12)
			} else {
				require.Equal(t, int64(test.wantDuration), gjson.GetBytes(forwarded, "duration").Int())
			}
			require.True(t, strings.Contains(string(forwarded), "content"))
		})
	}
}

func TestSeedanceFrames289ReservationCoversExactDuration(t *testing.T) {
	body := []byte(`{"model":"video","content":[{"type":"text","text":"waves"}],"frames":289}`)
	_, info, err := NormalizeSeedanceSubmission(body, "doubao-seedance-1-0-pro")
	require.NoError(t, err)
	require.Equal(t, 13, SeedanceReservationDuration(info, "doubao-seedance-1-0-pro"))
	svc, repo, input := videoTaskServiceFixture(t)
	input.DurationSeconds = info.DurationSecondsExact
	input.Frames = info.Frames
	input.MaxDurationSeconds = SeedanceReservationDuration(info, "doubao-seedance-1-0-pro")
	v := acceptVideoFixture(t, svc, input)
	require.GreaterOrEqual(t, v.HoldAmount, .1*289/24)
	_, err = svc.ObserveVideoTaskBilling(context.Background(), v.Protocol, v.TaskID, &OpenAIForwardResult{AsyncTaskStatus: "completed", VideoCount: 1}, false)
	require.NoError(t, err)
	require.InDelta(t, .1*289/24, repo.settlements[0].Usage.ActualCost, 1e-12)
}
