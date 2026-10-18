package service

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const EnterpriseDepartmentMaxDepth = 8

// ParseEnterpriseDepartmentPath parses the persisted, root-to-self scope snapshot.
func ParseEnterpriseDepartmentPath(path string) ([]int64, error) {
	ids := []int64{}
	if path == "" {
		return ids, nil
	}
	if !strings.HasPrefix(path, "/") || !strings.HasSuffix(path, "/") {
		return nil, fmt.Errorf("invalid department path")
	}
	for _, part := range strings.Split(strings.Trim(path, "/"), "/") {
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 || slices.Contains(ids, id) {
			return nil, fmt.Errorf("invalid department path")
		}
		ids = append(ids, id)
	}
	if EnterpriseDepartmentPath(ids) != path {
		return nil, fmt.Errorf("invalid department path")
	}
	if len(ids) > EnterpriseDepartmentMaxDepth {
		return nil, ErrEnterpriseDepartmentDepthExceeded
	}
	return ids, nil
}

func EnterpriseDepartmentPath(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	var p strings.Builder
	_ = p.WriteByte('/')
	for _, id := range ids {
		_, _ = p.WriteString(strconv.FormatInt(id, 10))
		_ = p.WriteByte('/')
	}
	return p.String()
}

// FrozenEnterpriseDepartmentIDs supports pre-tree snapshots without querying live
// membership. New authentication and batch snapshots always carry the full chain.
func FrozenEnterpriseDepartmentIDs(path []int64, departmentID int64) []int64 {
	if len(path) > 0 {
		return slices.Clone(path)
	}
	if departmentID > 0 {
		return []int64{departmentID}
	}
	return []int64{}
}

func enterpriseDepartmentInSubtree(c *EnterpriseMemberContext, departmentID int64) bool {
	return c != nil && c.DepartmentID != nil && (departmentID == *c.DepartmentID || slices.Contains(c.DepartmentSubtreeIDs, departmentID))
}

// ValidateFrozenEnterpriseDepartmentIDs validates shape without looking up the
// live tree: reparenting and archived scopes must not change an in-flight bill.
func ValidateFrozenEnterpriseDepartmentIDs(path []int64, departmentID int64) error {
	if len(path) == 0 {
		if departmentID < 0 {
			return fmt.Errorf("invalid department identity")
		}
		return nil
	}
	parsed, err := ParseEnterpriseDepartmentPath(EnterpriseDepartmentPath(path))
	if err != nil {
		return err
	}
	if departmentID <= 0 || parsed[len(parsed)-1] != departmentID {
		return fmt.Errorf("department path does not end at payer department")
	}
	return nil
}

func EnterpriseMemberDepartmentPathName(member *EnterpriseMember) string {
	if len(member.DepartmentPathNames) > 0 {
		return strings.Join(member.DepartmentPathNames, "/")
	}
	return member.DepartmentName
}
