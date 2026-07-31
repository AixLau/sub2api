package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RewardCampaignVersion stores a complete, immutable campaign configuration.
type RewardCampaignVersion struct {
	ent.Schema
}

func (RewardCampaignVersion) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "reward_campaign_versions"}}
}

func (RewardCampaignVersion) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("campaign_id").Immutable(),
		field.Int("version_number").Positive().Immutable(),
		field.JSON("config", map[string]any{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}).
			Immutable(),
		field.String("config_hash").
			MaxLen(64).
			NotEmpty().
			Immutable(),
		field.Int64("created_by").Optional().Nillable().Immutable(),
		field.Time("created_at").
			Default(time.Now).
			Immutable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (RewardCampaignVersion) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("campaign", RewardCampaign.Type).
			Ref("versions").
			Field("campaign_id").
			Unique().
			Required().
			Immutable(),
		edge.To("current_for_campaigns", RewardCampaign.Type),
		edge.To("grants", UserRewardGrant.Type),
		edge.To("jobs", RewardCampaignJob.Type),
	}
}

func (RewardCampaignVersion) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("campaign_id", "version_number").Unique(),
		index.Fields("campaign_id", "created_at"),
	}
}
