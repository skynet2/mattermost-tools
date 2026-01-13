package dashboard

import (
	"fmt"

	"github.com/user/mattermost-tools/internal/database"
)

func CalculateDeployOrder(repos []database.ReleaseRepo) (map[uint]int, error) {
	nameToID := make(map[string]uint)
	for _, r := range repos {
		nameToID[r.RepoName] = r.ID
	}

	deps := make(map[uint][]uint)
	for _, r := range repos {
		depNames, err := r.GetDependsOn()
		if err != nil {
			return nil, err
		}
		for _, depName := range depNames {
			if depID, ok := nameToID[depName]; ok {
				deps[r.ID] = append(deps[r.ID], depID)
			}
		}
	}

	order := make(map[uint]int)
	visited := make(map[uint]bool)
	inStack := make(map[uint]bool)

	var visit func(id uint) (int, error)
	visit = func(id uint) (int, error) {
		if inStack[id] {
			return 0, fmt.Errorf("circular dependency detected")
		}
		if visited[id] {
			return order[id], nil
		}

		inStack[id] = true

		maxDepOrder := 0
		for _, depID := range deps[id] {
			depOrder, err := visit(depID)
			if err != nil {
				return 0, err
			}
			if depOrder > maxDepOrder {
				maxDepOrder = depOrder
			}
		}

		inStack[id] = false
		visited[id] = true
		order[id] = maxDepOrder + 1

		return order[id], nil
	}

	for _, r := range repos {
		if _, err := visit(r.ID); err != nil {
			return nil, err
		}
	}

	return order, nil
}
