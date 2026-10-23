//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type videoBillingAdminHandlerRepo struct {
	service.VideoBillingReservationRepository
	reservation *service.VideoBillingReservation
	settlements []*service.VideoReservationSettlement
	releases    int
}

func (r *videoBillingAdminHandlerRepo) Get(_ context.Context, id string) (*service.VideoBillingReservation, error) {
	if r.reservation == nil || r.reservation.ID != id {
		return nil, service.ErrVideoReservationNotFound
	}
	copy := *r.reservation
	return &copy, nil
}
func (r *videoBillingAdminHandlerRepo) ListVideoBillingReservations(_ context.Context, state string, _, _ int) ([]*service.VideoBillingReservation, int, error) {
	if r.reservation.State != state {
		return nil, 0, nil
	}
	return []*service.VideoBillingReservation{r.reservation}, 1, nil
}
func (r *videoBillingAdminHandlerRepo) Settle(_ context.Context, input *service.VideoReservationSettlement) (*service.VideoReservationResult, error) {
	r.reservation.State, r.reservation.ActualAmount = service.VideoReservationSettled, input.Usage.ActualCost
	r.settlements = append(r.settlements, input)
	return &service.VideoReservationResult{Applied: true, ActualAmount: input.Usage.ActualCost}, nil
}
func (r *videoBillingAdminHandlerRepo) Release(_ context.Context, _, reason string) (*service.VideoReservationResult, error) {
	r.reservation.State, r.reservation.LastError = service.VideoReservationReleased, reason
	r.releases++
	return &service.VideoReservationResult{Applied: true}, nil
}

func videoBillingAdminRouter(t *testing.T, repo *videoBillingAdminHandlerRepo) (*gin.Engine, *middleware.AuthSubject, *map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := &service.OpenAIGatewayService{}
	svc.SetVideoBillingReservationRepository(repo)
	h := &OpenAIGatewayHandler{gatewayService: svc}
	router := gin.New()
	var actor middleware.AuthSubject
	var audit map[string]any
	router.Use(func(c *gin.Context) {
		if c.GetHeader("X-Test-Auth") != "none" {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
			role := c.GetHeader("X-Test-Role")
			if role == "" {
				role = service.RoleAdmin
			}
			c.Set(string(middleware.ContextKeyUserRole), role)
		}
		c.Next()
		actor, _ = middleware.GetAuthSubjectFromContext(c)
		action, _ := c.Get("audit_action")
		extra, _ := c.Get("audit_extra")
		audit = map[string]any{"action": action, "extra": extra}
	})
	group := router.Group("/api/v1/admin/video-billing-reservations")
	group.GET("", h.ListVideoBillingReservations)
	group.POST("/:reservation_id/release", h.ReleaseVideoBillingReservation)
	group.POST("/:reservation_id/settle", h.SettleVideoBillingReservation)
	return router, &actor, &audit
}

func videoAdminHandlerFixture(t *testing.T) *videoBillingAdminHandlerRepo {
	t.Helper()
	contract, err := json.Marshal(&service.VideoPricingContract{Version: service.VideoPricingContractVersion, Model: "frozen-video-model", Resolution: "720p", Mode: service.BillingModeVideo,
		UnitPrice: .1, EffectiveMultiplier: 1, ChannelTimeMultiplier: 1, ReasoningMultiplier: 1, MaxDurationSeconds: 6, MaxVideoCount: 1, ReservedAmount: .6})
	require.NoError(t, err)
	return &videoBillingAdminHandlerRepo{reservation: &service.VideoBillingReservation{ID: "reservation-1", UserID: 42, APIKeyID: 43, GroupID: 44, AccountID: 45, Protocol: service.VideoProtocolSeedance,
		Platform: service.PlatformVideo, State: service.VideoReservationManual, HoldAmount: .6, Contract: contract, Snapshot: json.RawMessage(`{"model":"original-model","password":"snapshot-secret"}`)}}
}

func videoAdminHTTPRequest(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	request := httptest.NewRequest(method, "/api/v1/admin/video-billing-reservations"+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, request)
	return w
}

func TestVideoBillingAdminHandlerPermissions(t *testing.T) {
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, ""}, {http.MethodPost, "/reservation-1/release"}, {http.MethodPost, "/reservation-1/settle"},
	} {
		for _, tc := range []struct {
			auth, role string
			want       int
		}{{"none", "", http.StatusUnauthorized}, {"", service.RoleUser, http.StatusForbidden}} {
			repo := videoAdminHandlerFixture(t)
			router, _, _ := videoBillingAdminRouter(t, repo)
			req := httptest.NewRequest(route.method, "/api/v1/admin/video-billing-reservations"+route.path, nil)
			req.Header.Set("X-Test-Auth", tc.auth)
			req.Header.Set("X-Test-Role", tc.role)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, tc.want, w.Code, w.Body.String())
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"), route.method+route.path)
			require.Empty(t, repo.settlements)
			require.Zero(t, repo.releases)
		}
	}
}

func TestVideoBillingAdminHandlerSafeListingAndValidation(t *testing.T) {
	repo := videoAdminHandlerFixture(t)
	router, actor, audit := videoBillingAdminRouter(t, repo)
	w := videoAdminHTTPRequest(router, http.MethodGet, "?state=manual&page=1&page_size=10", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Contains(t, w.Body.String(), "frozen-video-model")
	for _, secret := range []string{"snapshot-secret", "contract", "snapshot", "credentials", "token_pricing"} {
		require.NotContains(t, w.Body.String(), secret)
	}
	require.Equal(t, int64(7), actor.UserID)
	require.Equal(t, "admin.video_billing_reservations.list", (*audit)["action"])
	for _, query := range []string{"?page=abc", "?page=0", "?page_size=101", "?state=invalid"} {
		w := videoAdminHTTPRequest(router, http.MethodGet, query, "")
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestVideoBillingAdminHandlerRejectsAmountAndIdentityInjection(t *testing.T) {
	repo := videoAdminHandlerFixture(t)
	router, _, _ := videoBillingAdminRouter(t, repo)
	for _, body := range []string{
		`{}`, `{"reason":"   "}`, `{"reason":"verified","actual_amount":0.01}`, `{"reason":"verified","unit_price":0.01}`,
		`{"reason":"verified","actor_id":42}`, `{"reason":"verified","user_id":42}`, `{"reason":"verified"} {"reason":"second"}`,
		`{"reason":"verified","duration_seconds":0,"video_count":1}`, `{"reason":"verified","duration_seconds":6}`,
		`{"reason":"verified","duration_seconds":6,"video_count":2147483648}`, `{"reason":"verified","duration_seconds":6,"video_count":1,"completion_tokens":2147483648}`,
	} {
		w := videoAdminHTTPRequest(router, http.MethodPost, "/reservation-1/settle", body)
		require.Equal(t, http.StatusBadRequest, w.Code, body+": "+w.Body.String())
	}
	for _, body := range []string{`{}`, `{"reason":""}`, `{"reason":"verified","actual_amount":0}`} {
		w := videoAdminHTTPRequest(router, http.MethodPost, "/reservation-1/release", body)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
	require.Empty(t, repo.settlements)
	require.Zero(t, repo.releases)
}

func TestVideoBillingAdminHandlerSettlementKeepsAdminAuditAndOriginalPayer(t *testing.T) {
	repo := videoAdminHandlerFixture(t)
	router, actor, audit := videoBillingAdminRouter(t, repo)
	w := videoAdminHTTPRequest(router, http.MethodPost, "/reservation-1/settle", `{"reason":"verified actual output","duration_seconds":60,"video_count":2}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, repo.settlements, 1)
	require.Equal(t, int64(7), actor.UserID)
	require.Equal(t, int64(7), repo.settlements[0].ManualActorID)
	require.Equal(t, int64(42), repo.settlements[0].Usage.UserID)
	require.InDelta(t, .6, repo.settlements[0].Usage.ActualCost, 1e-12)
	require.Equal(t, "grok-video:reservation:reservation-1", repo.settlements[0].Usage.RequestID)
	require.Empty(t, repo.reservation.TaskID)
	require.Equal(t, "admin.video_billing_reservations.settle", (*audit)["action"])
	extra := (*audit)["extra"].(map[string]any)
	require.Equal(t, "reservation-1", extra["reservation_id"])
	require.Equal(t, "verified actual output", extra["resolution_reason"])
	w = videoAdminHTTPRequest(router, http.MethodPost, "/reservation-1/settle", `{"reason":"retry"}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, repo.settlements, 1)
}

func TestVideoBillingAdminHandlerManualOnlyAndReleaseIdempotency(t *testing.T) {
	repo := videoAdminHandlerFixture(t)
	router, _, _ := videoBillingAdminRouter(t, repo)
	repo.reservation.State = service.VideoReservationAccepted
	for _, action := range []string{"release", "settle"} {
		w := videoAdminHTTPRequest(router, http.MethodPost, "/reservation-1/"+action, `{"reason":"verified"}`)
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	}
	repo.reservation.State = service.VideoReservationManual
	for i := 0; i < 2; i++ {
		w := videoAdminHTTPRequest(router, http.MethodPost, "/reservation-1/release", `{"reason":"verified failure"}`)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	require.Equal(t, 1, repo.releases)
	require.Contains(t, repo.reservation.LastError, "administrator 7: verified failure")
	w := videoAdminHTTPRequest(router, http.MethodPost, "/missing/release", `{"reason":"verified failure"}`)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}
