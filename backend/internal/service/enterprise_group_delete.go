package service

import "context"

// EnterpriseGroupReferenceUserReader is a narrow addition for group deletion.
// Removing the last pool/whitelist row changes the restriction on other groups,
// so all members of the referencing enterprises need fresh auth snapshots.
type EnterpriseGroupReferenceUserReader interface {
	GetEnterpriseGroupAffectedUserIDs(ctx context.Context, groupID int64) ([]int64, error)
}

func enterpriseGroupAffectedUserIDs(ctx context.Context, repo GroupRepository, groupID int64) ([]int64, error) {
	if reader, ok := repo.(EnterpriseGroupReferenceUserReader); ok {
		return reader.GetEnterpriseGroupAffectedUserIDs(ctx, groupID)
	}
	// Existing non-enterprise repository implementations and test doubles keep
	// their original contract without widening GroupRepository.
	return nil, nil
}
