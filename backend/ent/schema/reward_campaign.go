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

// RewardCampaign is the mutable control-plane record for a reward campaign.
// Published configuration lives in immutable RewardCampaignVersion records.
type RewardCampaign struct {
	ent.Schema
}

func (RewardCampaign) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "reward_campaigns"}}
}

func (RewardCampaign) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (RewardCampaign) Fields() []ent.Field {
	return []ent.Field{
		field.String("system_key").
			MaxLen(64).
			Optional().
			Nillable().
			Immutable(),
		field.String("name").
			MaxLen(200).
			NotEmpty(),
		field.String("description").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Default(""),
		field.String("status").
			MaxLen(20).
			Default("draft").
			Validate(validateRewardCampaignStatus),
		field.String("issuance_mode").
			MaxLen(32).
			Default("on_access").
			Validate(validateRewardCampaignMode),
		field.String("timezone").
			MaxLen(64).
			Default("UTC").
			NotEmpty(),
		field.Time("starts_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("ends_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int("priority").Default(0),
		field.Float("total_budget").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Default(0).
			Min(0),
		field.Float("reserved_budget").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Default(0).
			Min(0),
		field.Float("spent_budget").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Default(0).
			Min(0),
		field.Float("released_budget").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Default(0).
			Min(0),
		field.Int64("current_version_id").Optional().Nillable(),
		field.Int64("created_by").Optional().Nillable(),
		field.Int64("updated_by").Optional().Nillable(),
		field.Time("published_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("paused_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("ended_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("archived_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (RewardCampaign) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("versions", RewardCampaignVersion.Type),
		edge.From("current_version", RewardCampaignVersion.Type).
			Ref("current_for_campaigns").
			Field("current_version_id").
			Unique(),
		edge.To("grants", UserRewardGrant.Type),
		edge.To("user_states", RewardCampaignUserState.Type),
		edge.To("jobs", RewardCampaignJob.Type),
	}
}

func (RewardCampaign) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("system_key").Unique().Annotations(entsql.IndexWhere("system_key IS NOT NULL")),
		index.Fields("status", "starts_at", "ends_at"),
		index.Fields("issuance_mode", "status"),
		index.Fields("priority"),
	}
}

func validateRewardCampaignStatus(value string) error {
	switch value {
	case "draft", "scheduled", "active", "paused", "ended", "archived":
		return nil
	default:
		return fmt.Errorf("unsupported reward campaign status %q", value)
	}
}

func validateRewardCampaignMode(value string) error {
	switch value {
	case "on_access", "scheduled_batch":
		return nil
	default:
		return fmt.Errorf("unsupported reward campaign issuance mode %q", value)
	}
}
