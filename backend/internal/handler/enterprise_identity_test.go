//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnterpriseIdentityOnCurrentUserAndProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	visibleBalance := 19.5
	for _, identity := range []*service.EnterpriseIdentity{nil,
		{ID: 2, Name: "Acme", Role: "member", MemberStatus: "active", EnterpriseStatus: "active", FrozenBalance: &visibleBalance, Balance: &visibleBalance},
		{ID: 2, Name: "Acme", Role: "owner", BalanceVisible: true, FrozenBalance: &visibleBalance, Balance: &visibleBalance, MemberStatus: "disabled", EnterpriseStatus: "disabled"},
	} {
		for _, currentUser := range []bool{true, false} {
			repo := &userHandlerRepoStub{user: &service.User{ID: 11, Email: "enterprise@example.com", Role: service.RoleUser, Status: service.StatusActive}}
			users := service.NewUserService(repo, nil, nil, nil)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 11})
			if currentUser {
				h := NewAuthHandler(nil, nil, users, nil, nil, nil, nil, nil)
				h.enterpriseIdentity = identityReaderStub{identity: identity}
				h.GetCurrentUser(c)
			} else {
				h := NewUserHandler(users, nil, nil, nil, nil, nil)
				h.enterpriseIdentity = identityReaderStub{identity: identity}
				h.GetProfile(c)
			}
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var result struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
			var expected *service.EnterpriseIdentity
			if identity != nil {
				copy := *identity
				if !copy.BalanceVisible {
					copy.Balance = nil
					copy.FrozenBalance = nil
				}
				expected = &copy
			}
			want, err := json.Marshal(expected)
			require.NoError(t, err)
			require.JSONEq(t, string(want), string(result.Data["enterprise"]))
		}
	}
}

func TestEnterpriseIdentityLookupErrorFailsProfile(t *testing.T) {
	h := NewUserHandler(service.NewUserService(&userHandlerRepoStub{user: &service.User{ID: 1, Email: "user@example.com"}}, nil, nil, nil), nil, nil, nil, nil, nil)
	h.enterpriseIdentity = identityReaderStub{err: errors.New("unavailable")}
	_, err := h.buildUserProfileResponse(context.Background(), 1, &service.User{ID: 1, Email: "user@example.com"})
	require.Error(t, err)
}
