package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RewardCampaignUserState throttles campaign evaluation and tracks per-user limits.
type RewardCampaignUserState struct {
	ent.Schema
}

func (RewardCampaignUserState) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "reward_campaign_user_states"}}
}

func (RewardCampaignUserState) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (RewardCampaignUserState) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("campaign_id").Immutable(),
		field.Int64("user_id").Immutable(),
		field.Time("last_evaluated_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_won_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_granted_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_claimed_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("next_eligible_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int64("evaluation_count").Default(0).NonNegative(),
		field.Int64("win_count").Default(0).NonNegative(),
		field.Int64("grant_count").Default(0).NonNegative(),
		field.Int64("claim_count").Default(0).NonNegative(),
		field.Bool("control_group").Default(false),
		field.String("current_cycle_key").MaxLen(128).Default(""),
	}
}

func (RewardCampaignUserState) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("campaign", RewardCampaign.Type).
			Ref("user_states").
			Field("campaign_id").
			Unique().
			Required().
			Immutable(),
		edge.From("user", User.Type).
			Ref("reward_campaign_states").
			Field("user_id").
			Unique().
			Required().
			Immutable(),
	}
}

func (RewardCampaignUserState) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("campaign_id", "user_id").Unique(),
		index.Fields("campaign_id", "next_eligible_at"),
		index.Fields("user_id", "updated_at"),
	}
}
