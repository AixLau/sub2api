package securityaudit

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

type captureCountingJobRepository struct {
	fakeJobRepository
	captures atomic.Int32
}

func (r *captureCountingJobRepository) RecordCapture(context.Context, PromptSnapshot, int64, int) (*Event, error) {
	r.captures.Add(1)
	return nil, nil
}

// The request-path scope check must not drop selected-user captures: they are
// taken even while prompt auditing is off and regardless of the audit groups.
func TestPromptServiceEnqueueKeepsSelectedUserCaptureOutsideAuditScope(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
	}{
		{name: "audit off", enabled: false},
		{name: "async audit, group out of scope", enabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := asyncConfig()
			cfg.Enabled = tc.enabled
			cfg.AllGroups = false
			cfg.GroupIDs = []int64{1}
			cfg.CaptureUsers = []CaptureUser{{UserID: 9}}
			repo := &captureCountingJobRepository{}
			svc := memoryTestPromptService(cfg, repo)
			groupID := int64(2)
			req := asyncRequest()
			req.GroupID = &groupID
			req.UserID = 9

			require.NoError(t, svc.Enqueue(context.Background(), req))
			svc.enqueueWG.Wait()

			require.Equal(t, int32(1), repo.captures.Load())
		})
	}
}
