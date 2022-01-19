package steps

import (
	"fmt"

	"github.com/git-town/git-town/v7/src/git"
)

// SyncBranchSteps provides the steps to sync the branch with the given name.
func SyncBranchSteps(branchName string, pushBranch bool, repo *git.ProdRepo) (result StepList, err error) {
	isFeature := repo.Config.IsFeatureBranch(branchName)
	hasRemoteOrigin, err := repo.Silent.HasRemote("origin")
	if err != nil {
		return result, err
	}
	if !hasRemoteOrigin && !isFeature {
		return
	}
	result.Append(&CheckoutBranchStep{BranchName: branchName})
	if isFeature {
		steps, err := syncFeatureBranchSteps(branchName, repo)
		if err != nil {
			return result, err
		}
		result.AppendList(steps)
	} else {
		steps, err := syncNonFeatureBranchSteps(branchName, repo)
		if err != nil {
			return result, err
		}
		result.AppendList(steps)
	}
	if pushBranch && hasRemoteOrigin && !repo.Config.IsOffline() {
		hasTrackingBranch, err := repo.Silent.HasTrackingBranch(branchName)
		if err != nil {
			return result, err
		}
		if hasTrackingBranch {
			result.Append(&PushBranchStep{BranchName: branchName})
		} else {
			result.Append(&CreateTrackingBranchStep{BranchName: branchName})
		}
	}
	return result, nil
}

// Helpers

func syncFeatureBranchSteps(branchName string, repo *git.ProdRepo) (result StepList, err error) {
	hasTrackingBranch, err := repo.Silent.HasTrackingBranch(branchName)
	if err != nil {
		return result, err
	}
	if hasTrackingBranch {
		result.Append(&MergeBranchStep{BranchName: repo.Silent.TrackingBranchName(branchName)})
	}
	result.Append(&MergeBranchStep{BranchName: repo.Config.ParentBranch(branchName)})
	return
}

func syncNonFeatureBranchSteps(branchName string, repo *git.ProdRepo) (result StepList, err error) {
	hasTrackingBranch, err := repo.Silent.HasTrackingBranch(branchName)
	if err != nil {
		return result, err
	}
	if hasTrackingBranch {
		if repo.Config.PullBranchStrategy() == "rebase" {
			result.Append(&RebaseBranchStep{BranchName: repo.Silent.TrackingBranchName(branchName)})
		} else {
			result.Append(&MergeBranchStep{BranchName: repo.Silent.TrackingBranchName(branchName)})
		}
	}

	mainBranchName := repo.Config.MainBranch()
	hasUpstream, err := repo.Silent.HasRemote("upstream")
	if err != nil {
		return result, err
	}
	if mainBranchName == branchName && hasUpstream && repo.Config.ShouldSyncUpstream() {
		result.Append(&FetchUpstreamStep{BranchName: mainBranchName})
		result.Append(&RebaseBranchStep{BranchName: fmt.Sprintf("upstream/%s", mainBranchName)})
	}
	return
}
