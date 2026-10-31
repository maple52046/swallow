package convergence

import (
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
)

// ConditionDoc is the stored shape of a Condition, shared by every migrated resource's collection
// so conditions read back the same everywhere.
type ConditionDoc struct {
	Type               string    `bson:"type"`
	Status             string    `bson:"status"`
	Reason             string    `bson:"reason"`
	Message            string    `bson:"message,omitempty"`
	LastTransitionTime time.Time `bson:"lastTransitionTime"`
	ObservedGeneration int64     `bson:"observedGeneration"`
}

// TargetDoc is the stored shape of a TargetStatus: a resource's realization on one target.
type TargetDoc struct {
	TargetID           string         `bson:"targetId"`
	ObservedGeneration int64          `bson:"observedGeneration"`
	ObservedAt         *time.Time     `bson:"observedAt,omitempty"`
	Conditions         []ConditionDoc `bson:"conditions,omitempty"`
}

// ConditionDocs maps conditions onto their stored shape.
func ConditionDocs(conditions []convergence.Condition) []ConditionDoc {
	docs := make([]ConditionDoc, len(conditions))
	for i, c := range conditions {
		docs[i] = ConditionDoc{
			Type: string(c.Type), Status: string(c.Status), Reason: c.Reason, Message: c.Message,
			LastTransitionTime: c.LastTransitionTime.UTC(), ObservedGeneration: c.ObservedGeneration,
		}
	}
	return docs
}

// ToConditions maps stored conditions back onto the domain.
func ToConditions(docs []ConditionDoc) []convergence.Condition {
	conditions := make([]convergence.Condition, len(docs))
	for i, d := range docs {
		conditions[i] = convergence.Condition{
			Type: convergence.ConditionType(d.Type), Status: convergence.ConditionStatus(d.Status), Reason: d.Reason,
			Message: d.Message, LastTransitionTime: d.LastTransitionTime, ObservedGeneration: d.ObservedGeneration,
		}
	}
	return conditions
}

// TargetDocFrom maps a TargetStatus onto its stored shape.
func TargetDocFrom(target convergence.TargetStatus) TargetDoc {
	var observedAt *time.Time
	if target.ObservedAt != nil {
		at := target.ObservedAt.UTC()
		observedAt = &at
	}
	return TargetDoc{
		TargetID: target.TargetID, ObservedGeneration: target.ObservedGeneration,
		ObservedAt: observedAt, Conditions: ConditionDocs(target.Conditions),
	}
}

// ToTargetStatus maps a stored target back onto the domain.
func ToTargetStatus(doc TargetDoc) convergence.TargetStatus {
	return convergence.TargetStatus{
		TargetID: doc.TargetID, ObservedGeneration: doc.ObservedGeneration,
		ObservedAt: doc.ObservedAt, Conditions: ToConditions(doc.Conditions),
	}
}
