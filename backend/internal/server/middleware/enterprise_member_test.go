//go:build unit

package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type enterpriseMemberReaderStub struct {
	member *service.EnterpriseMemberContext
	err    error
	userID int64
}

func (s *enterpriseMemberReaderStub) GetMemberContextByUserID(_ context.Context, userID int64) (*service.EnterpriseMemberContext, error) {
	s.userID = userID
	return s.member, s.err
}

func TestEnterpriseMemberAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, method, enterpriseStatus, memberStatus string
		anonymous, notMember                         bool
		err                                          error
		status                                       int
		reason                                       string
	}{
		{name: "active read", method: http.MethodGet, enterpriseStatus: "active", memberStatus: "active", status: 200},
		{name: "active write", method: http.MethodPost, enterpriseStatus: "active", memberStatus: "active", status: 200},
		{name: "disabled enterprise read", method: http.MethodGet, enterpriseStatus: "disabled", memberStatus: "active", status: 200},
		{name: "disabled enterprise write", method: http.MethodPost, enterpriseStatus: "disabled", memberStatus: "active", status: 403, reason: "ENTERPRISE_DISABLED"},
		{name: "disabled member read", method: http.MethodGet, enterpriseStatus: "active", memberStatus: "disabled", status: 403, reason: "ENTERPRISE_MEMBER_DISABLED"},
		{name: "disabled member write", method: http.MethodPost, enterpriseStatus: "disabled", memberStatus: "disabled", status: 403, reason: "ENTERPRISE_MEMBER_DISABLED"},
		{name: "not member", method: http.MethodGet, notMember: true, status: 403, reason: "ENTERPRISE_NOT_MEMBER"},
		{name: "anonymous", method: http.MethodGet, anonymous: true, status: 401},
		{name: "storage unavailable", method: http.MethodGet, err: errors.New("database unavailable"), status: 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &enterpriseMemberReaderStub{member: &service.EnterpriseMemberContext{EnterpriseID: 8, EnterpriseStatus: tc.enterpriseStatus, MemberStatus: tc.memberStatus}, err: tc.err}
			if tc.notMember {
				reader.member = nil
			}
			r := gin.New()
			r.Use(func(c *gin.Context) {
				if !tc.anonymous {
					c.Set(string(ContextKeyUser), AuthSubject{UserID: 42})
				}
				c.Next()
			})
			r.Use(EnterpriseMemberAuth(reader))
			called := false
			r.Handle(tc.method, "/", func(c *gin.Context) {
				called = true
				member, ok := GetEnterpriseMemberFromContext(c)
				require.True(t, ok)
				require.Equal(t, int64(8), member.EnterpriseID)
				c.Status(200)
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, "/", nil))
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.status == 200, called)
			if tc.reason != "" {
				require.Contains(t, w.Body.String(), tc.reason)
			}
			if !tc.anonymous {
				require.Equal(t, int64(42), reader.userID)
			}
		})
	}
}
