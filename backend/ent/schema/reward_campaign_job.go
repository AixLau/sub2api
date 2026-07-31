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

// RewardCampaignJob is a durable, lease-based batch campaign run.
type RewardCampaignJob struct {
	ent.Schema
}

func (RewardCampaignJob) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "reward_campaign_jobs"}}
}

func (RewardCampaignJob) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (RewardCampaignJob) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("campaign_id").Immutable(),
		field.Int64("campaign_version_id").Immutable(),
		field.String("job_type").MaxLen(32).Default("issue_batch").Immutable().Validate(func(value string) error {
			switch value {
			case "issue_batch", "expire_grants", "rollup_behavior":
				return nil
			default:
				return fmt.Errorf("unsupported reward campaign job type %q", value)
			}
		}),
		field.String("idempotency_key").MaxLen(128).NotEmpty().Unique().Immutable(),
		field.String("status").MaxLen(24).Default("pending").Validate(validateRewardCampaignJobStatus),
		field.Int64("cursor_user_id").Default(0).NonNegative(),
		field.Int64("max_user_id").Default(0).NonNegative().Immutable(),
		field.String("lease_owner").MaxLen(128).Default(""),
		field.Time("lease_expires_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("scheduled_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("next_attempt_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("started_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("finished_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int("attempt_count").Default(0).NonNegative(),
		field.Int("max_attempts").Default(20).Positive(),
		field.Int64("total_users").Default(0).NonNegative(),
		field.Int64("scanned_users").Default(0).NonNegative(),
		field.Int64("matched_users").Default(0).NonNegative(),
		field.Int64("granted_users").Default(0).NonNegative(),
		field.Int64("skipped_users").Default(0).NonNegative(),
		field.Int64("failed_users").Default(0).NonNegative(),
		field.String("last_error").SchemaType(map[string]string{dialect.Postgres: "text"}).Default(""),
	}
}

func (RewardCampaignJob) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("campaign", RewardCampaign.Type).
			Ref("jobs").
			Field("campaign_id").
			Unique().
			Required().
			Immutable(),
		edge.From("campaign_version", RewardCampaignVersion.Type).
			Ref("jobs").
			Field("campaign_version_id").
			Unique().
			Required().
			Immutable(),
		edge.To("grants", UserRewardGrant.Type).
			Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}

func (RewardCampaignJob) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("campaign_id", "status"),
		index.Fields("status", "next_attempt_at"),
		index.Fields("lease_expires_at"),
	}
}

func validateRewardCampaignJobStatus(value string) error {
	switch value {
	case "pending", "processing", "paused", "retry", "succeeded", "failed", "dead_letter", "cancelled":
		return nil
	default:
		return fmt.Errorf("unsupported reward campaign job status %q", value)
	}
}
