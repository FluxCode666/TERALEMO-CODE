package middleware

import (
	"context"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const enterpriseMemberContextKey = "enterprise_member"

// EnterpriseMemberContextReader keeps panel membership loading separate from
// gateway authentication and its cached snapshots.
type EnterpriseMemberContextReader interface {
	GetMemberContextByUserID(context.Context, int64) (*service.EnterpriseMemberContext, error)
}

// EnterpriseMemberAuth runs after JWT authentication. Disabled enterprises keep
// read access, while disabled members cannot access any enterprise endpoint.
func EnterpriseMemberAuth(reader EnterpriseMemberContextReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		subject, ok := GetAuthSubjectFromContext(c)
		if !ok {
			response.Unauthorized(c, "User not found in context")
			c.Abort()
			return
		}
		member, err := reader.GetMemberContextByUserID(c.Request.Context(), subject.UserID)
		if err != nil {
			response.ErrorFrom(c, err)
			c.Abort()
			return
		}
		if member == nil {
			response.ErrorFrom(c, service.ErrEnterpriseNotMember)
			c.Abort()
			return
		}
		if member.MemberStatus != service.EnterpriseMemberStatusActive {
			response.ErrorFrom(c, service.ErrEnterpriseMemberDisabled)
			c.Abort()
			return
		}
		readOnly := c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions
		if !readOnly && member.EnterpriseStatus != service.EnterpriseStatusActive {
			response.ErrorFrom(c, service.ErrEnterpriseDisabled)
			c.Abort()
			return
		}
		c.Set(enterpriseMemberContextKey, member)
		c.Next()
	}
}

func GetEnterpriseMemberFromContext(c *gin.Context) (*service.EnterpriseMemberContext, bool) {
	value, ok := c.Get(enterpriseMemberContextKey)
	if !ok {
		return nil, false
	}
	member, ok := value.(*service.EnterpriseMemberContext)
	return member, ok && member != nil
}
