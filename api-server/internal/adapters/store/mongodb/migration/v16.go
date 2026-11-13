package migration

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// v16DefaultPool is the name of the Pool each Site's v16 merge keeps: the per-Site Pools named after
// the provisioner's default resource pool become one installation-wide Pool.
const v16DefaultPool = "default"

// v16Pool is the part of a stored Pool v16 reads.
type v16Pool struct {
	ID   string `bson:"_id"`
	Name string `bson:"name"`
}

// v16Policy is a stored Pool Sudo Policy; its id is the Pool's.
type v16Policy struct {
	ID               string `bson:"_id"`
	MemberSudo       bool   `bson:"memberSudo"`
	SudoPasswordless bool   `bson:"sudoPasswordless"`
}

// v16Membership is the part of a stored Pool Membership v16 reads.
type v16Membership struct {
	ID     string `bson:"_id"`
	PoolID string `bson:"poolId"`
	UserID string `bson:"userId"`
	Role   string `bson:"role"`
}

// v16DeletesPools reports whether v16 would delete a Pool: any Pool not named `default`, whose
// memberships, Sudo Policy, and placements go with it. Only then does the upgrade need a backup
// confirmation; merging the `default` Pools loses no access.
func v16DeletesPools(ctx context.Context, db *mongo.Database) (bool, error) {
	count, err := db.Collection("pools").CountDocuments(ctx, bson.M{"name": bson.M{"$ne": v16DefaultPool}})
	return count > 0, err
}

// migrateV15ToV16 makes Pools installation-wide and swallow-only (decision 082):
//
//  1. Every Pool named `default` (one per Site at most) is merged into the oldest of them: its
//     memberships move there (a User already there keeps the higher Pool Role and the kept
//     membership's sudo overrides), the kept Sudo Policy takes the stricter value of each setting
//     (a Pool without one counts as no member sudo and sudo with a password), the Servers placed in it
//     are placed in the kept Pool, and then it is deleted.
//  2. Every other Pool is deleted.
//  3. Whatever still names a Pool that no longer exists is removed: memberships and Sudo Policies are
//     deleted and Server placements cleared, leaving those Servers in no Pool (admins only). The
//     POSIX group numbers allocated to deleted Pools stay allocated, as when the API deletes a Pool.
//  4. Pools lose siteId, generation, and status, memberships lose siteId, and the per-Site unique
//     index on Pools is dropped; the API creates the installation-wide one at startup.
//
// The provisioner is never contacted, so its resource pools and machines are left as they are. Each
// step only moves references to Pools that still exist before deleting any Pool, and step 3 sweeps by
// what remains, so an interrupted run resumes safely and a second run changes nothing.
func migrateV15ToV16(ctx context.Context, db *mongo.Database) error {
	pools := db.Collection("pools")
	cursor, err := pools.Find(ctx, bson.M{"name": v16DefaultPool},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return err
	}
	var defaults []v16Pool
	if err := cursor.All(ctx, &defaults); err != nil {
		return err
	}
	if len(defaults) > 1 {
		kept := defaults[0].ID
		if err := v16MergePolicies(ctx, db, kept, defaults[1:]); err != nil {
			return err
		}
		for _, merged := range defaults[1:] {
			if err := v16MergeMemberships(ctx, db, kept, merged.ID); err != nil {
				return err
			}
			if _, err := db.Collection("servers").UpdateMany(ctx, bson.M{"spec.poolId": merged.ID},
				bson.M{"$set": bson.M{"spec.poolId": kept}}); err != nil {
				return err
			}
			if _, err := pools.DeleteOne(ctx, bson.M{"_id": merged.ID}); err != nil {
				return err
			}
		}
	}
	if _, err := pools.DeleteMany(ctx, bson.M{"name": bson.M{"$ne": v16DefaultPool}}); err != nil {
		return err
	}

	remaining, err := v16PoolIDs(ctx, pools)
	if err != nil {
		return err
	}
	if _, err := db.Collection("pool_memberships").DeleteMany(ctx, bson.M{"poolId": bson.M{"$nin": remaining}}); err != nil {
		return err
	}
	if _, err := db.Collection("pool_access_policies").DeleteMany(ctx, bson.M{"_id": bson.M{"$nin": remaining}}); err != nil {
		return err
	}
	if _, err := db.Collection("servers").UpdateMany(ctx,
		bson.M{"spec.poolId": bson.M{"$exists": true, "$nin": append([]string{""}, remaining...)}},
		bson.M{"$unset": bson.M{"spec.poolId": ""}}); err != nil {
		return err
	}

	if _, err := pools.UpdateMany(ctx, bson.M{},
		bson.M{"$unset": bson.M{"siteId": "", "generation": "", "status": ""}}); err != nil {
		return err
	}
	if _, err := db.Collection("pool_memberships").UpdateMany(ctx, bson.M{"siteId": bson.M{"$exists": true}},
		bson.M{"$unset": bson.M{"siteId": ""}}); err != nil {
		return err
	}
	return dropIndexIfExists(ctx, pools, "pool_site_name")
}

// v16PoolIDs returns the ids of every stored Pool, never nil, so it can stand in a $nin.
func v16PoolIDs(ctx context.Context, pools *mongo.Collection) ([]string, error) {
	cursor, err := pools.Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, err
	}
	var docs []v16Pool
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(docs))
	for _, doc := range docs {
		ids = append(ids, doc.ID)
	}
	return ids, nil
}

// v16MergePolicies gives the kept Pool the stricter of every merged Pool's Sudo Policy, then deletes
// the merged Pools' policies. A Pool with no stored policy has the zero value (no member sudo, sudo
// with a password), which is already the strictest, so the kept Pool's policy is only ever narrowed.
func v16MergePolicies(ctx context.Context, db *mongo.Database, kept string, merged []v16Pool) error {
	policies := db.Collection("pool_access_policies")
	var keptPolicy v16Policy
	err := policies.FindOne(ctx, bson.M{"_id": kept}).Decode(&keptPolicy)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return err
	}
	ids := make([]string, 0, len(merged))
	for _, pool := range merged {
		ids = append(ids, pool.ID)
	}
	if err == nil {
		memberSudo, passwordless := keptPolicy.MemberSudo, keptPolicy.SudoPasswordless
		for _, id := range ids {
			var policy v16Policy
			switch err := policies.FindOne(ctx, bson.M{"_id": id}).Decode(&policy); {
			case errors.Is(err, mongo.ErrNoDocuments):
				memberSudo, passwordless = false, false
			case err != nil:
				return err
			default:
				memberSudo = memberSudo && policy.MemberSudo
				passwordless = passwordless && policy.SudoPasswordless
			}
		}
		if _, err := policies.UpdateOne(ctx, bson.M{"_id": kept},
			bson.M{"$set": bson.M{"memberSudo": memberSudo, "sudoPasswordless": passwordless}}); err != nil {
			return err
		}
	}
	_, err = policies.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": ids}})
	return err
}

// v16MergeMemberships moves the memberships of the merged Pool to the kept one. A User who is
// already a member of the kept Pool keeps that membership, raised to owner when either was an owner,
// and the merged membership is deleted; the (poolId, userId) unique index therefore never conflicts.
func v16MergeMemberships(ctx context.Context, db *mongo.Database, kept, merged string) error {
	memberships := db.Collection("pool_memberships")
	cursor, err := memberships.Find(ctx, bson.M{"poolId": merged})
	if err != nil {
		return err
	}
	var moving []v16Membership
	if err := cursor.All(ctx, &moving); err != nil {
		return err
	}
	for _, membership := range moving {
		var existing v16Membership
		switch err := memberships.FindOne(ctx, bson.M{"poolId": kept, "userId": membership.UserID}).Decode(&existing); {
		case errors.Is(err, mongo.ErrNoDocuments):
			if _, err := memberships.UpdateOne(ctx, bson.M{"_id": membership.ID},
				bson.M{"$set": bson.M{"poolId": kept}}); err != nil {
				return err
			}
			continue
		case err != nil:
			return err
		}
		if membership.Role == "owner" && existing.Role != "owner" {
			if _, err := memberships.UpdateOne(ctx, bson.M{"_id": existing.ID},
				bson.M{"$set": bson.M{"role": "owner"}}); err != nil {
				return err
			}
		}
		if _, err := memberships.DeleteOne(ctx, bson.M{"_id": membership.ID}); err != nil {
			return err
		}
	}
	return nil
}
