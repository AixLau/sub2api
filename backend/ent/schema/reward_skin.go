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

// RewardSkin stores validated, same-origin scratch-card image content.
type RewardSkin struct {
	ent.Schema
}

func (RewardSkin) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "reward_skins"}}
}

func (RewardSkin) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (RewardSkin) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(120).NotEmpty(),
		field.String("description").SchemaType(map[string]string{dialect.Postgres: "text"}).Default(""),
		field.String("alt_text").MaxLen(255).Default(""),
		field.String("status").MaxLen(20).Default("active").Validate(func(value string) error {
			switch value {
			case "active", "inactive", "archived":
				return nil
			default:
				return fmt.Errorf("unsupported reward skin status %q", value)
			}
		}),
		field.String("mime_type").MaxLen(32).Immutable().Validate(func(value string) error {
			switch value {
			case "image/png", "image/jpeg", "image/webp":
				return nil
			default:
				return fmt.Errorf("unsupported reward skin MIME type %q", value)
			}
		}),
		field.Int("width").Positive().Immutable(),
		field.Int("height").Positive().Immutable(),
		field.Int64("byte_size").Positive().Max(1024 * 1024).Immutable(),
		field.String("sha256").MaxLen(64).NotEmpty().Unique().Immutable(),
		field.Bytes("content").Immutable(),
		field.Int64("created_by").Optional().Nillable().Immutable(),
		field.Int64("updated_by").Optional().Nillable(),
		field.Time("archived_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (RewardSkin) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("grants", UserRewardGrant.Type).
			Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}

func (RewardSkin) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "created_at"),
	}
}
