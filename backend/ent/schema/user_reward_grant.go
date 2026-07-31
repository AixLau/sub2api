package schema

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UserRewardGrant is the immutable-value reward card issued to a user.
// Amount and presentation snapshots never follow later campaign edits.
type UserRewardGrant struct {
	ent.Schema
}

func (UserRewardGrant) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "user_reward_grants"}}
}

func (UserRewardGrant) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (UserRewardGrant) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("campaign_id").Immutable(),
		field.Int64("campaign_version_id").Immutable(),
		field.Int64("user_id").Immutable(),
		field.Int64("skin_id").Optional().Nillable().Immutable(),
		field.Int64("job_id").Optional().Nillable().Immutable(),
		field.String("cycle_key").MaxLen(128).NotEmpty().Immutable(),
		field.String("source").MaxLen(32).NotEmpty().Immutable().Validate(validateRewardGrantSource),
		field.String("status").MaxLen(20).Default("pending").Validate(validateRewardGrantStatus),
		field.Float("amount").SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).Positive().Immutable(),
		field.Int("priority").Default(0).Immutable(),
		field.JSON("copy_snapshot", map[string]any{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}).Immutable(),
		field.JSON("skin_snapshot", map[string]any{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}).Immutable(),
		field.JSON("metadata", map[string]any{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}).Immutable(),
		field.Time("expires_at").Optional().Nillable().Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("viewed_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("claimed_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("expired_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Float("balance_after").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Int64("claim_record_id").Optional().Nillable(),
		field.String("claim_reference").MaxLen(128).Default(""),
	}
}

func (UserRewardGrant) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("campaign", RewardCampaign.Type).
			Ref("grants").
			Field("campaign_id").
			Unique().
			Required().
			Immutable(),
		edge.From("campaign_version", RewardCampaignVersion.Type).
			Ref("grants").
			Field("campaign_version_id").
			Unique().
			Required().
			Immutable(),
		edge.From("user", User.Type).
			Ref("reward_grants").
			Field("user_id").
			Unique().
			Required().
			Immutable(),
		edge.From("skin", RewardSkin.Type).
			Ref("grants").
			Field("skin_id").
			Unique().
			Immutable(),
		edge.From("job", RewardCampaignJob.Type).
			Ref("grants").
			Field("job_id").
			Unique().
			Immutable(),
		edge.From("claim_record", RedeemCode.Type).
			Ref("reward_grant").
			Field("claim_record_id").
			Unique(),
	}
}

func (UserRewardGrant) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("campaign_id", "user_id", "cycle_key").Unique(),
		index.Fields("user_id", "status", "priority", "expires_at"),
		index.Fields("campaign_id", "status", "created_at"),
		index.Fields("status", "expires_at"),
		index.Fields("job_id"),
		index.Fields("claim_record_id").Unique().Annotations(entsql.IndexWhere("claim_record_id IS NOT NULL")),
	}
}

func validateRewardGrantStatus(value string) error {
	switch value {
	case "pending", "claimed", "expired", "cancelled":
		return nil
	default:
		return fmt.Errorf("unsupported reward grant status %q", value)
	}
}

func validateRewardGrantSource(value string) error {
	switch value {
	case "on_access", "scheduled_batch", "legacy_welcome", "legacy_surprise", "manual":
		return nil
	default:
		return fmt.Errorf("unsupported reward grant source %q", value)
	}
}
