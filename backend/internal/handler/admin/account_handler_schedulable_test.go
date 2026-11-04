//go:build unit

package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminAccountEditBindsOptionalSchedulable(t *testing.T) {
	for _, body := range []string{`{"schedulable":true}`, `{"schedulable":false}`, `{}`} {
		t.Run(body, func(t *testing.T) {
			stub := newStubAdminService()
			router := setupAccountMixedChannelRouter(stub)
			request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/accounts/42", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.NotNil(t, stub.lastUpdateAccountInput)
			if body == `{}` {
				require.Nil(t, stub.lastUpdateAccountInput.Schedulable)
			} else {
				require.NotNil(t, stub.lastUpdateAccountInput.Schedulable)
				require.Equal(t, strings.Contains(body, "true"), *stub.lastUpdateAccountInput.Schedulable)
			}
		})
	}
}
