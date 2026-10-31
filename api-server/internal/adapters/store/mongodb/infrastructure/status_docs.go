package infrastructure

import (
	convergencestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/convergence"
	infradomain "github.com/maple52046/swallow/internal/core/infrastructure/domain"
)

// groupStatusDoc is the stored status of a Zone or Pool, written only by the catalog controller.
type groupStatusDoc struct {
	ObservedGeneration int64                           `bson:"observedGeneration"`
	Conditions         []convergencestore.ConditionDoc `bson:"conditions,omitempty"`
	Provisioners       []groupRealizationDoc           `bson:"provisioners,omitempty"`
}

type groupRealizationDoc struct {
	convergencestore.TargetDoc `bson:",inline"`
	ProviderName               string `bson:"providerName,omitempty"`
}

func toGroupStatusDoc(status infradomain.GroupStatus) groupStatusDoc {
	doc := groupStatusDoc{
		ObservedGeneration: status.ObservedGeneration,
		Conditions:         convergencestore.ConditionDocs(status.Conditions),
	}
	for _, r := range status.Provisioners {
		doc.Provisioners = append(doc.Provisioners, groupRealizationDoc{
			TargetDoc: convergencestore.TargetDocFrom(r.TargetStatus), ProviderName: r.ProviderName,
		})
	}
	return doc
}

func toGroupStatus(doc groupStatusDoc) infradomain.GroupStatus {
	status := infradomain.GroupStatus{
		ObservedGeneration: doc.ObservedGeneration,
		Conditions:         convergencestore.ToConditions(doc.Conditions),
	}
	for _, r := range doc.Provisioners {
		status.Provisioners = append(status.Provisioners, infradomain.GroupRealization{
			TargetStatus: convergencestore.ToTargetStatus(r.TargetDoc), ProviderName: r.ProviderName,
		})
	}
	return status
}
