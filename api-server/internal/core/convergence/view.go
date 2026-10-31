package convergence

import "github.com/maple52046/swallow/internal/shared/wire"

// ConditionItem is the published shape of a Condition, the same for every migrated resource
// (desired-state-model.md §4.2, §11.1). Times are RFC 3339 in UTC.
type ConditionItem struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	Message            string `json:"message"`
	LastTransitionTime string `json:"lastTransitionTime"`
	ObservedGeneration int64  `json:"observedGeneration"`
}

// ConditionItems maps conditions onto their published shape; it never returns nil, so a resource
// with no judgement yet serializes an empty array rather than null.
func ConditionItems(conditions []Condition) []ConditionItem {
	items := make([]ConditionItem, 0, len(conditions))
	for _, c := range conditions {
		items = append(items, ConditionItem{
			Type: string(c.Type), Status: string(c.Status), Reason: c.Reason, Message: c.Message,
			LastTransitionTime: wire.Time(c.LastTransitionTime), ObservedGeneration: c.ObservedGeneration,
		})
	}
	return items
}
