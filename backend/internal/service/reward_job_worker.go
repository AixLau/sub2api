package service

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type RewardJobWorker struct {
	repo      RewardRepository
	settings  *SettingService
	workerID  string
	interval  time.Duration
	lease     time.Duration
	batchSize int
	stop      chan struct{}
	done      chan struct{}
	stopOnce  sync.Once
}

var errRewardCampaignsDisabled = errors.New("reward campaigns are disabled")

func NewRewardJobWorker(repo RewardRepository, settings *SettingService) *RewardJobWorker {
	return &RewardJobWorker{
		repo:      repo,
		settings:  settings,
		workerID:  newRewardWorkerID(),
		interval:  15 * time.Second,
		lease:     2 * time.Minute,
		batchSize: 500,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
}

func ProvideRewardJobWorker(repo RewardRepository, settings *SettingService) *RewardJobWorker {
	worker := NewRewardJobWorker(repo, settings)
	worker.Start()
	return worker
}

func (w *RewardJobWorker) Start() {
	go w.run()
}

func (w *RewardJobWorker) Stop() {
	w.stopOnce.Do(func() { close(w.stop) })
	<-w.done
}

func (w *RewardJobWorker) run() {
	defer close(w.done)
	w.runOnce(context.Background())
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			w.runOnce(context.Background())
		}
	}
}

func (w *RewardJobWorker) runOnce(ctx context.Context) {
	now := time.Now().UTC()
	// Expiration and reservation release are financial maintenance and must
	// continue even while the rollout switch is off.
	if err := w.repo.EndExpiredCampaigns(ctx, now); err != nil {
		slog.Error("end expired reward campaigns failed", "error", err)
		return
	}
	if !w.rewardCampaignsEnabled(ctx) {
		return
	}
	if _, err := w.repo.EnqueueScheduledCampaigns(ctx, now); err != nil {
		slog.Error("enqueue scheduled reward campaigns failed", "error", err)
		return
	}
	jobs, err := w.repo.ClaimJobs(ctx, w.workerID, now, 1, w.lease)
	if err != nil {
		slog.Error("claim reward campaign job failed", "error", err)
		return
	}
	for _, job := range jobs {
		if err := w.processJob(ctx, job); err != nil {
			if errors.Is(err, errRewardCampaignsDisabled) {
				if releaseErr := w.repo.ReleaseJob(ctx, job.ID, w.workerID, time.Now().UTC()); releaseErr != nil {
					slog.Error("release disabled reward campaign job failed", "job_id", job.ID, "error", releaseErr)
				}
				continue
			}
			if retryErr := w.repo.RetryJob(ctx, job.ID, w.workerID, time.Now().UTC(), err); retryErr != nil {
				slog.Error("retry reward campaign job failed", "job_id", job.ID, "error", retryErr)
			}
		}
	}
}

func (w *RewardJobWorker) processJob(ctx context.Context, job RewardCampaignJob) error {
	campaign, err := w.repo.GetCampaignVersion(ctx, job.CampaignID, job.VersionID)
	if err != nil {
		return err
	}
	if campaign.Status != RewardCampaignStatusActive {
		return fmt.Errorf("campaign %d is not active", campaign.ID)
	}
	if campaign.IssuanceMode != RewardIssuanceModeScheduledBatch {
		return fmt.Errorf("campaign %d is not a scheduled batch campaign", campaign.ID)
	}
	cursor := job.CursorUserID
	for {
		select {
		case <-w.stop:
			return errors.New("reward job worker stopped")
		default:
		}
		if !w.rewardCampaignsEnabled(ctx) {
			return errRewardCampaignsDisabled
		}
		latest, err := w.repo.GetCampaignVersion(ctx, job.CampaignID, job.VersionID)
		if err != nil {
			return err
		}
		if latest.Status != RewardCampaignStatusActive {
			return fmt.Errorf("campaign %d is no longer active", latest.ID)
		}
		userIDs, err := w.repo.ListBatchCandidateUserIDs(ctx, campaign.ID, cursor, job.MaxUserID, w.batchSize)
		if err != nil {
			return err
		}
		if len(userIDs) == 0 {
			return w.repo.CompleteJob(ctx, job.ID, w.workerID, time.Now().UTC())
		}
		now := time.Now().UTC()
		profiles, err := w.repo.GetAudienceProfiles(ctx, userIDs, now)
		if err != nil {
			return err
		}
		var scanned, matched, granted, skipped int64
		for _, userID := range userIDs {
			scanned++
			profile, ok := profiles[userID]
			if !ok || IsEmailPlusAlias(profile.Email) || !RewardAudienceMatches(ResolveRewardAudienceRelativeTimes(campaign.Config.Audience, now), profile) {
				skipped++
				continue
			}
			matched++
			control := rewardControlGroup(campaign.ID, userID, campaign.ControlGroupPercent)
			shouldAward := !control && secureProbability(campaign.WinProbability)
			grant, _, err := w.repo.EvaluateAndMaybeGrant(
				ctx, *campaign, userID, now, shouldAward, control,
				RewardGrantSourceScheduledBatch, &job.ID,
			)
			if err != nil {
				return err
			}
			if grant != nil {
				granted++
			} else {
				skipped++
			}
		}
		cursor = userIDs[len(userIDs)-1]
		if err := w.repo.ExtendJobLease(
			ctx, job.ID, w.workerID, cursor, scanned, matched, granted, skipped, 0,
			time.Now().UTC(), w.lease,
		); err != nil {
			return err
		}
		if len(userIDs) < w.batchSize {
			return w.repo.CompleteJob(ctx, job.ID, w.workerID, time.Now().UTC())
		}
	}
}

func (w *RewardJobWorker) rewardCampaignsEnabled(ctx context.Context) bool {
	return w.settings != nil && w.settings.IsRewardCampaignsEnabled(ctx)
}

func newRewardWorkerID() string {
	var raw [8]byte
	if _, err := cryptorand.Read(raw[:]); err == nil {
		return "reward-" + hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("reward-%d", time.Now().UnixNano())
}
