//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type usageEnterpriseIdentityStub struct {
	identity *service.EnterpriseIdentity
	err      error
	userID   int64
}

func (s *usageEnterpriseIdentityStub) Identity(_ context.Context, userID int64) (*service.EnterpriseIdentity, error) {
	s.userID = userID
	return s.identity, s.err
}

func TestGatewayUsageEnterpriseBalanceVisibilityInAllModes(t *testing.T) {
	for _, role := range []string{service.EnterpriseRoleOwner, service.EnterpriseRoleManager, service.EnterpriseRoleMember} {
		for _, visibleSetting := range []bool{false, true} {
			for _, mode := range []string{"wallet", "quota", "subscription"} {
				t.Run(role+"/"+mode+"/"+map[bool]string{false: "hidden", true: "visible"}[visibleSetting], func(t *testing.T) {
					balance := 27.5
					visible := service.EnterpriseBalanceVisible(role, service.EnterpriseSettings{BalanceVisibleToManager: visibleSetting, BalanceVisibleToMember: visibleSetting})
					identity := &service.EnterpriseIdentity{ID: 7, Name: "Company", Role: role, EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusActive, BalanceVisible: visible}
					if visible {
						identity.Balance = &balance
					}
					reader := &usageEnterpriseIdentityStub{identity: identity}
					h := &GatewayHandler{enterpriseIdentityReader: reader} // A personal wallet read would panic.
					c, w := newEnterpriseUsageContext()
					key := &service.APIKey{ID: 2, UserID: 42, User: &service.User{ID: 42, Balance: 999, Enterprise: &service.EnterpriseMemberContext{EnterpriseID: 7}}}
					subject := middleware.AuthSubject{UserID: 42}
					switch mode {
					case "quota":
						key.Quota = 100
						h.usageQuotaLimited(c, c.Request.Context(), key, nil, nil, nil)
					case "subscription":
						key.Group = &service.Group{SubscriptionType: service.SubscriptionTypeSubscription}
						c.Set(string(middleware.ContextKeySubscription), &service.UserSubscription{})
						h.usageUnrestricted(c, c.Request.Context(), key, subject, nil, nil, nil)
					default:
						h.usageUnrestricted(c, c.Request.Context(), key, subject, nil, nil, nil)
					}
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					require.Equal(t, int64(42), reader.userID)
					var response map[string]any
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
					require.Equal(t, visible, response["balance_visible"])
					for _, field := range []string{"balance", "remaining"} {
						require.Contains(t, response, field)
						if visible {
							require.Equal(t, balance, response[field])
						} else {
							require.Nil(t, response[field])
						}
					}
					require.NotContains(t, w.Body.String(), "999")
					if mode == "quota" {
						require.Equal(t, 100.0, response["quota"].(map[string]any)["remaining"], "key quota remains available separately")
					}
				})
			}
		}
	}
}

func TestGatewayUsageEnterpriseIdentityFailureNeverReturnsPersonalWallet(t *testing.T) {
	for _, reader := range []enterpriseUsageIdentityReader{nil, &usageEnterpriseIdentityStub{}, &usageEnterpriseIdentityStub{err: errors.New("offline")}} {
		c, w := newEnterpriseUsageContext()
		h := &GatewayHandler{enterpriseIdentityReader: reader}
		h.usageUnrestricted(c, c.Request.Context(), &service.APIKey{User: &service.User{Balance: 999, Enterprise: &service.EnterpriseMemberContext{EnterpriseID: 7}}}, middleware.AuthSubject{UserID: 42}, nil, nil, nil)
		require.Equal(t, http.StatusServiceUnavailable, w.Code)
		require.NotContains(t, w.Body.String(), "999")
	}
}

func TestGatewayUsageCurrentMembershipOverridesOldPersonalAuthSnapshot(t *testing.T) {
	balance := 8.0
	reader := &usageEnterpriseIdentityStub{identity: &service.EnterpriseIdentity{ID: 7, EnterpriseStatus: service.EnterpriseStatusActive, MemberStatus: service.EnterpriseMemberStatusActive, BalanceVisible: true, Balance: &balance}}
	c, w := newEnterpriseUsageContext()
	(&GatewayHandler{enterpriseIdentityReader: reader}).usageUnrestricted(c, c.Request.Context(), &service.APIKey{User: &service.User{Balance: 999}}, middleware.AuthSubject{UserID: 42}, nil, nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"balance":8`)
	require.NotContains(t, w.Body.String(), "999")
}

func newEnterpriseUsageContext() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
	return c, w
}
