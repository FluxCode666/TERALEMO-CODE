//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type enterpriseGroupDeleteRepoStub struct {
	GroupRepository
	deleted            bool
	err                error
	lookupErr          error
	lookupBeforeDelete bool
}

func (s *enterpriseGroupDeleteRepoStub) GetByID(context.Context, int64) (*Group, error) {
	return &Group{ID: 1}, nil
}
func (s *enterpriseGroupDeleteRepoStub) GetEnterpriseGroupAffectedUserIDs(context.Context, int64) ([]int64, error) {
	s.lookupBeforeDelete = !s.deleted
	return []int64{11, 22}, s.lookupErr
}
func (s *enterpriseGroupDeleteRepoStub) Delete(context.Context, int64) error {
	if s.err != nil {
		return s.err
	}
	s.deleted = true
	return nil
}
func (s *enterpriseGroupDeleteRepoStub) DeleteCascade(ctx context.Context, id int64) ([]int64, error) {
	return nil, s.Delete(ctx, id)
}
func (s *enterpriseGroupDeleteRepoStub) DeleteCascadeIfEmpty(ctx context.Context, id int64) ([]int64, error) {
	return nil, s.Delete(ctx, id)
}

type enterpriseGroupDeleteKeysStub struct{ APIKeyRepository }

func (enterpriseGroupDeleteKeysStub) ListKeysByGroupID(context.Context, int64) ([]string, error) {
	return []string{"group-key"}, nil
}

type enterpriseGroupDeleteInvalidatorStub struct {
	APIKeyAuthCacheInvalidator
	repo                    *enterpriseGroupDeleteRepoStub
	users                   []int64
	groupIDs                []int64
	keys                    []string
	invalidatedBeforeDelete bool
}

func (s *enterpriseGroupDeleteInvalidatorStub) check() {
	if !s.repo.deleted {
		s.invalidatedBeforeDelete = true
	}
}
func (s *enterpriseGroupDeleteInvalidatorStub) InvalidateAuthCacheByUserID(_ context.Context, id int64) {
	s.check()
	s.users = append(s.users, id)
}
func (s *enterpriseGroupDeleteInvalidatorStub) InvalidateAuthCacheByGroupID(_ context.Context, id int64) {
	s.check()
	s.groupIDs = append(s.groupIDs, id)
}
func (s *enterpriseGroupDeleteInvalidatorStub) InvalidateAuthCacheByKey(_ context.Context, key string) {
	s.check()
	s.keys = append(s.keys, key)
}

func TestEnterpriseGroupDeleteInvalidatesAllAffectedMembersAfterSuccess(t *testing.T) {
	for _, kind := range []string{"group-service", "admin", "admin-empty"} {
		for _, failure := range []string{"", "lookup", "delete"} {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				repo := &enterpriseGroupDeleteRepoStub{}
				cause := errors.New("deletion failed")
				if failure == "lookup" {
					repo.lookupErr = cause
				}
				if failure == "delete" {
					repo.err = cause
				}
				auth := &enterpriseGroupDeleteInvalidatorStub{repo: repo}
				var err error
				switch kind {
				case "group-service":
					err = NewGroupService(repo, auth).Delete(context.Background(), 1)
				case "admin", "admin-empty":
					svc := &adminServiceImpl{groupRepo: repo, emptyGroupDeleteRepo: repo, apiKeyRepo: enterpriseGroupDeleteKeysStub{}, authCacheInvalidator: auth}
					if kind == "admin-empty" {
						err = svc.DeleteGroupIfEmpty(context.Background(), 1)
					} else {
						err = svc.DeleteGroup(context.Background(), 1)
					}
				}
				require.True(t, repo.lookupBeforeDelete)
				require.False(t, auth.invalidatedBeforeDelete)
				if failure != "" {
					require.ErrorIs(t, err, cause)
					require.Empty(t, auth.users)
					require.Empty(t, auth.keys)
					require.Empty(t, auth.groupIDs)
					return
				}
				require.NoError(t, err)
				require.Equal(t, []int64{11, 22}, auth.users)
				require.True(t, repo.deleted)
			})
		}
	}
}
