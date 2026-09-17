package service

import (
	"context"
	"time"
)

type CredentialInstanceAddInput struct {
	CreateCredentialInstanceInput
	GroupIDs          []int64    `json:"group_ids"`
	ReplaceInstanceID int64      `json:"replace_instance_id"`
	DrainDeadline     *time.Time `json:"drain_deadline"`
}
type CredentialInstanceLifecycle interface {
	AddCredentialInstance(context.Context, int64, int64, int64, string, CredentialInstanceAddInput) (int64, error)
}
