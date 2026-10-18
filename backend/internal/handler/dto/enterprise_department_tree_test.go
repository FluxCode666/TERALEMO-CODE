package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseTreeDepartmentMemberAndOverviewContract(t *testing.T) {
	parent := int64(7)
	now := time.Now()
	d := EnterpriseDepartmentFromService(&service.EnterpriseDepartment{ID: 11, EnterpriseID: 1, ParentID: &parent, PathIDs: []int64{3, 7, 11}, PathNames: []string{"集团", "技术部", "研发组"}, Depth: 3, MemberCount: 2, TotalMemberCount: 5})
	data, err := json.Marshal(d)
	require.NoError(t, err)
	require.JSONEq(t, `{"id":11,"enterprise_id":1,"parent_id":7,"path_ids":[3,7,11],"path_names":["集团","技术部","研发组"],"depth":3,"name":"","sort_order":0,"member_count":2,"total_member_count":5,"created_at":"0001-01-01T00:00:00Z"}`, string(data))
	member := &service.EnterpriseMember{DepartmentID: &parent, DepartmentName: "技术部", DepartmentPathNames: []string{"集团", "技术部"}, LastLoginAt: &now}
	m := EnterpriseMemberFromService(member)
	require.Equal(t, member.DepartmentPathNames, m.DepartmentPathNames)
	require.Equal(t, &now, m.LastLoginAt)
	overview := EnterpriseOverviewFromService(&service.EnterpriseOverviewData{Enterprise: &service.Enterprise{}, Me: member})
	require.Equal(t, "集团/技术部", *overview.Me.DepartmentName)
	require.Equal(t, member.DepartmentPathNames, overview.Me.DepartmentPathNames)
	empty := EnterpriseMemberFromService(&service.EnterpriseMember{})
	require.NotNil(t, empty.DepartmentPathNames)
	require.Empty(t, empty.DepartmentPathNames)
	require.Nil(t, empty.LastLoginAt)
}
