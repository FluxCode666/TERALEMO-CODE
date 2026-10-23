//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const seedanceDraftTestBody = `{"model":"video","content":[{"type":"draft_task","draft_task":{"id":"draft-1"}}],"resolution":"1080p"}`
const seedanceDraftTestStatus = `{"id":"draft-1","model":"doubao-seedance-2-5-pro","status":"succeeded","draft":true,"resolution":"480p","duration":7}`

type seedanceDraftSourceRepo struct {
	VideoBillingReservationRepository
	source *VideoBillingReservation
}

func (r *seedanceDraftSourceRepo) GetByTask(_ context.Context, protocol, taskID string) (*VideoBillingReservation, error) {
	if r.source == nil || r.source.Protocol != protocol || r.source.TaskID != taskID {
		return nil, ErrVideoReservationNotFound
	}
	return r.source, nil
}

type seedanceDraftPendingCache struct {
	GatewayCache
	pending map[string][]byte
	owners  map[int64]map[string]int64
}

func (c *seedanceDraftPendingCache) GetGrokVideoPendingBilling(_ context.Context, key string) ([]byte, error) {
	return c.pending[key], nil
}

func (c *seedanceDraftPendingCache) GetSessionAccountID(_ context.Context, groupID int64, key string) (int64, error) {
	if id := c.owners[groupID][key]; id > 0 {
		return id, nil
	}
	return 0, ErrStickySessionNotFound
}

func (c *seedanceDraftPendingCache) SetSessionAccountID(_ context.Context, groupID int64, key string, accountID int64, _ time.Duration) error {
	if c.owners == nil {
		c.owners = map[int64]map[string]int64{}
	}
	if c.owners[groupID] == nil {
		c.owners[groupID] = map[string]int64{}
	}
	c.owners[groupID][key] = accountID
	return nil
}

func seedanceDraftFixture(t *testing.T) (*OpenAIGatewayService, *APIKey, *Account, *seedanceDraftSourceRepo, *seedanceDraftPendingCache, *grokMediaContentUpstreamStub) {
	t.Helper()
	groupID := int64(24)
	key := &APIKey{ID: 20, UserID: 10, User: &User{ID: 10}, GroupID: &groupID, Group: &Group{ID: groupID}}
	account := seedanceTestAccount()
	account.Status, account.Schedulable, account.GroupIDs = StatusActive, true, []int64{groupID}
	account.Credentials["model_mapping"] = map[string]any{"video": "doubao-seedance-2-5-pro", "doubao-seedance-2-5-pro": "must-not-map-twice"}
	snapshot, err := json.Marshal(VideoBillingSnapshot{Model: "video", UpstreamModel: "doubao-seedance-2-5-pro", Resolution: "480p", RequestedDurationSeconds: -1, VideoCount: 1})
	require.NoError(t, err)
	contract, err := json.Marshal(VideoPricingContract{Version: 1, Model: "video", Resolution: "480p", MaxDurationSeconds: 30, MaxVideoCount: 1})
	require.NoError(t, err)
	repo := &seedanceDraftSourceRepo{source: &VideoBillingReservation{ID: "source", UserID: 10, APIKeyID: 20, GroupID: groupID, AccountID: account.ID, Protocol: VideoProtocolSeedance, TaskID: "seedance:draft-1", State: VideoReservationSettled, Contract: contract, Snapshot: snapshot}}
	cache := &seedanceDraftPendingCache{pending: map[string][]byte{}}
	upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(seedanceDraftTestStatus)}
	svc := &OpenAIGatewayService{accountRepo: &stubOpenAIAccountRepo{accounts: []Account{*account}}, cache: cache, videoBillingRepo: repo, httpUpstream: upstream}
	return svc, key, account, repo, cache, upstream
}

func TestSeedanceDraftReceiptPreservesNativeBodyAndPinsInheritedInputs(t *testing.T) {
	svc, key, account, repo, _, upstream := seedanceDraftFixture(t)
	status, err := sjson.Set(seedanceDraftTestStatus, "frames", 181)
	require.NoError(t, err)
	upstream.responses = []*http.Response{grokMediaContentStatusResponse(status), grokMediaContentStatusResponse(`{"id":"formal-1"}`)}
	body := []byte(seedanceDraftTestBody)
	ctx, info, accountID, err := svc.PrepareSeedanceDraftSubmission(context.Background(), key, 10, body)
	require.NoError(t, err)
	require.Equal(t, account.ID, accountID)
	require.Equal(t, 181.0/24, info.DurationSecondsExact, "native frames override rounded duration")
	require.Equal(t, int(math.Ceil(181.0/24)), info.DurationSeconds)
	require.Equal(t, "1080p", info.Resolution)
	require.Equal(t, "video", info.Model)
	prepared, inherited, err := svc.NormalizeSeedanceSubmissionForAccount(ctx, body, account)
	require.NoError(t, err)
	require.Equal(t, body, prepared)
	require.Equal(t, info, inherited)
	require.False(t, gjson.GetBytes(prepared, "duration").Exists())
	c, _ := grokMediaContentTestContext(http.MethodPost, "/api/v3/contents/generations/tasks", nil)
	result, err := svc.ForwardSeedance(ctx, c, account, SeedanceEndpointCreate, "", prepared)
	require.NoError(t, err)
	require.Equal(t, info.DurationSecondsExact, result.VideoDurationSecondsExact)
	require.Len(t, upstream.requests, 2, "one status check, one paid creation")
	payload, err := io.ReadAll(upstream.requests[1].Body)
	require.NoError(t, err)
	require.Equal(t, "doubao-seedance-2-5-pro", gjson.GetBytes(payload, "model").String())
	require.False(t, gjson.GetBytes(payload, "duration").Exists())
	require.Equal(t, VideoReservationSettled, repo.source.State, "a new task does not mutate or reuse source settlement")
	for _, tc := range []struct {
		name string
		svc  *OpenAIGatewayService
		ctx  context.Context
		body []byte
		acct *Account
	}{
		{"no receipt", svc, context.Background(), body, account},
		{"different service", &OpenAIGatewayService{}, ctx, body, account},
		{"changed body", svc, ctx, []byte(seedanceDraftTestBody[:len(seedanceDraftTestBody)-1] + `,"watermark":true}`), account},
		{"different account", svc, ctx, body, &Account{ID: account.ID + 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.svc.NormalizeSeedanceSubmissionForAccount(tc.ctx, tc.body, tc.acct)
			globalErr := ErrSeedanceDraftTaskUnavailable
			require.ErrorIs(t, err, globalErr)
		})
	}
}

func TestSeedanceDraftSourceRequiresIdentityAndFrozenMappingBeforeStatus(t *testing.T) {
	for _, scenario := range []string{"unknown", "wrong-user", "wrong-key", "wrong-group", "wrong-public-model", "blocked-public-model", "released", "preparing", "account-missing", "mapping-changed", "account-group-changed", "missing-contract"} {
		t.Run(scenario, func(t *testing.T) {
			svc, key, _, repo, _, upstream := seedanceDraftFixture(t)
			body := []byte(seedanceDraftTestBody)
			switch scenario {
			case "unknown":
				repo.source = nil
			case "wrong-user":
				repo.source.UserID++
			case "wrong-key":
				repo.source.APIKeyID++
			case "wrong-group":
				repo.source.GroupID++
			case "wrong-public-model":
				body, _ = sjson.SetBytes(body, "model", "other-public-alias")
			case "blocked-public-model":
				key.Group.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"other-model"}}
			case "released":
				repo.source.State = VideoReservationReleased
			case "preparing":
				repo.source.State = VideoReservationPreparing
			case "account-missing":
				repo.source.AccountID++
			case "mapping-changed":
				svc.accountRepo.(*stubOpenAIAccountRepo).accounts[0].Credentials["model_mapping"] = map[string]any{"video": "different-model"}
			case "account-group-changed":
				svc.accountRepo.(*stubOpenAIAccountRepo).accounts[0].GroupIDs = []int64{25}
			case "missing-contract":
				repo.source.Contract = nil
			}
			_, _, _, err := svc.PrepareSeedanceDraftSubmission(context.Background(), key, 10, body)
			require.ErrorIs(t, err, ErrSeedanceDraftTaskUnavailable)
			require.Empty(t, upstream.requests, "inaccessible sources never cause upstream requests")
		})
	}
}

func TestSeedanceDraftLegacyPendingKeepsGroupAndAccountBinding(t *testing.T) {
	for _, scenario := range []string{"identity-snapshot", "pre-identity-pending", "smart-no-native-timing", "opaque-deployment", "wrong-group", "wrong-binding", "no-binding", "wrong-pending-user"} {
		t.Run(scenario, func(t *testing.T) {
			svc, key, account, repo, cache, upstream := seedanceDraftFixture(t)
			repo.source = nil
			pending := GrokVideoPendingBilling{Model: "video", UpstreamModel: "doubao-seedance-2-5-pro", VideoResolution: "480p", VideoDurationSeconds: -1, BillingIdentity: &GrokVideoBillingIdentity{UserID: 10, GroupID: 24}}
			if scenario == "pre-identity-pending" {
				pending.BillingIdentity = nil
			}
			if scenario == "wrong-group" {
				pending.BillingIdentity.GroupID++
			}
			if scenario == "wrong-pending-user" {
				pending.BillingIdentity.UserID++
			}
			if scenario == "smart-no-native-timing" {
				status, err := sjson.Delete(seedanceDraftTestStatus, "duration")
				require.NoError(t, err)
				upstream.response = grokMediaContentStatusResponse(status)
			}
			if scenario == "opaque-deployment" {
				pending.UpstreamModel = "ep-frozen-deployment"
				account.Credentials["model_mapping"] = map[string]any{"video": pending.UpstreamModel}
			}
			payload, err := json.Marshal(pending)
			require.NoError(t, err)
			cache.pending[grokVideoPendingBillingKey("seedance:draft-1", 10, 20)] = payload
			ownerID := account.ID
			if scenario == "wrong-binding" {
				ownerID++
			}
			if scenario != "no-binding" {
				require.NoError(t, svc.BindGrokMediaVideoRequestAccount(context.Background(), key.GroupID, "seedance:draft-1", 10, 20, ownerID))
			}
			_, info, boundID, err := svc.PrepareSeedanceDraftSubmission(context.Background(), key, 10, []byte(seedanceDraftTestBody))
			if scenario == "wrong-group" || scenario == "wrong-pending-user" || scenario == "wrong-binding" || scenario == "no-binding" {
				require.ErrorIs(t, err, ErrSeedanceDraftTaskUnavailable)
				require.Empty(t, upstream.requests)
				return
			}
			require.NoError(t, err)
			require.Equal(t, account.ID, boundID)
			want := 7
			if scenario == "smart-no-native-timing" {
				want = 30
			}
			require.Equal(t, want, info.DurationSeconds)
			require.Len(t, upstream.requests, 1)
		})
	}
}

func TestSeedanceDraftStatusMustBeSuccessfulNativeDraft(t *testing.T) {
	for _, tc := range []struct {
		field string
		value any
	}{
		{"id", "other-task"}, {"model", "different-model"}, {"status", "running"}, {"status", "failed"}, {"draft", false}, {"draft", "true"}, {"resolution", "720p"}, {"duration", 31},
	} {
		t.Run(tc.field+"/"+gjson.Parse(mustSeedanceDraftJSON(t, tc.value)).String(), func(t *testing.T) {
			svc, key, _, _, _, upstream := seedanceDraftFixture(t)
			status, err := sjson.Set(seedanceDraftTestStatus, tc.field, tc.value)
			require.NoError(t, err)
			upstream.response = grokMediaContentStatusResponse(status)
			_, _, _, err = svc.PrepareSeedanceDraftSubmission(context.Background(), key, 10, []byte(seedanceDraftTestBody))
			require.ErrorIs(t, err, ErrSeedanceDraftTaskUnavailable)
			require.Len(t, upstream.requests, 1, "only a non-billable source lookup")
		})
	}
}

func mustSeedanceDraftJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}

func TestSeedanceDraftInheritedFieldsAreNeverResubmitted(t *testing.T) {
	for _, field := range []string{"duration", "frames", "ratio", "seed", "generate_audio", "omni_reference_task_type", "prompt", "image_url"} {
		body, err := sjson.Set(seedanceDraftTestBody, field, 5)
		require.NoError(t, err)
		_, err = ParseSeedanceRequest([]byte(body))
		require.ErrorIs(t, err, ErrSeedanceDraftTaskUnavailable, field)
	}
	body := []byte(seedanceDraftTestBody)
	for _, field := range []string{"model", "resolution"} {
		missing, err := sjson.DeleteBytes(body, field)
		require.NoError(t, err)
		_, err = ParseSeedanceRequest(missing)
		require.Error(t, err, "missing %s cannot bypass source model or tier checks", field)
	}
	_, err := ParseSeedanceRequest(body)
	require.NoError(t, err, "the public parser accepts the official structure without guessing inherited timing")
	_, _, err = NormalizeSeedanceSubmission(body, "doubao-seedance-2-5-pro")
	require.ErrorIs(t, err, ErrSeedanceDraftTaskUnavailable, "ordinary normalizer cannot bypass the receipt")
	ordinary := []byte(`{"model":"video","content":[{"type":"text","text":"waves"}],"resolution":"480p","duration":6,"draft":true}`)
	prepared, info, err := NormalizeSeedanceSubmission(ordinary, "ep-opaque-deployment")
	require.NoError(t, err)
	require.Equal(t, 6, info.DurationSeconds)
	require.Equal(t, "480p", gjson.GetBytes(prepared, "resolution").String())
}
