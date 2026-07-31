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

// UserBehaviorDaily stores hourly user behavior buckets. The historical table
// name is retained while bucket_start makes the aggregation resolution explicit.
type UserBehaviorDaily struct {
	ent.Schema
}

func (UserBehaviorDaily) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "user_behavior_daily"}}
}

func (UserBehaviorDaily) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id").Immutable(),
		field.Time("bucket_start").Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int64("request_count").Default(0).NonNegative(),
		field.Float("actual_cost").Default(0).Min(0).SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Float("recharge_amount").Default(0).Min(0).SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Time("last_api_use_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_active_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (UserBehaviorDaily) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("behavior_daily").
			Field("user_id").
			Unique().
			Required().
			Immutable(),
	}
}

func (UserBehaviorDaily) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "bucket_start").Unique(),
		index.Fields("bucket_start", "user_id"),
		index.Fields("user_id", "last_api_use_at"),
	}
}
