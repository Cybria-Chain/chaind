// Copyright © 2021 - 2023 Weald Technology Limited.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package standard

import (
	"context"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
)

// OnFinalityUpdated is called when finality has been updated in the database.
// This is usually triggered by the finalizer.
func (s *Service) OnFinalityUpdated(
	ctx context.Context,
	finalizedEpoch phase0.Epoch,
) {
	log := log.With().Uint64("finalized_epoch", uint64(finalizedEpoch)).Logger()
	log.Trace().Msg("Handler called")

	// Only allow 1 handler to be active.
	acquired := s.activitySem.TryAcquire(1)
	if !acquired {
		log.Debug().Msg("Another handler running")
		return
	}
	defer s.activitySem.Release(1)

	if finalizedEpoch == 0 {
		log.Debug().Msg("Not summarizing on epoch 0")
		return
	}
	summaryEpoch := finalizedEpoch - 1

	if err := s.summarizeEpochs(ctx, summaryEpoch); err != nil {
		log.Warn().Err(err).Msg("Failed to update epochs")
		return
	}
	if err := s.summarizeBlocks(ctx, summaryEpoch); err != nil {
		log.Warn().Err(err).Msg("Failed to update blocks")
		return
	}
	if err := s.summarizeValidators(ctx, summaryEpoch); err != nil {
		log.Warn().Err(err).Msg("Failed to update validators")
		return
	}

	md, err := s.getMetadata(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to obtain metadata for day summarizer")
	}
	if md.PeriodicValidatorRollups {
		if err := s.summarizeValidatorDays(ctx); err != nil {
			log.Warn().Err(err).Msg("Failed to update validator days")
			return
		}

		if err := s.prune(ctx, summaryEpoch); err != nil {
			log.Warn().Err(err).Msg("Failed to prune summaries")
			return
		}
	}

	monitorEpochProcessed(finalizedEpoch)
	log.Trace().Msg("Finished handling finality checkpoint")
}

func (s *Service) summarizeEpochs(ctx context.Context, summaryEpoch phase0.Epoch) error {
	if !s.epochSummaries {
		return nil
	}

	md, err := s.getMetadata(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to obtain metadata for epoch summarizer")
	}

	lastEpoch := md.LastEpoch
	if lastEpoch != 0 {
		lastEpoch++
	}

	// Limit the number of epochs summarised per pass, if we are also pruning.
	maxEpochsPerRun := phase0.Epoch(s.maxDaysPerRun) * s.epochsPerDay()
	if s.validatorEpochRetention != nil && maxEpochsPerRun > 0 && summaryEpoch-lastEpoch > maxEpochsPerRun {
		summaryEpoch = lastEpoch + maxEpochsPerRun
	}

	log.Trace().Uint64("last_epoch", uint64(lastEpoch)).Uint64("summary_epoch", uint64(summaryEpoch)).Msg("Epochs catchup bounds")

	for epoch := lastEpoch; epoch <= summaryEpoch; epoch++ {
		updated, err := s.summarizeEpoch(ctx, md, epoch)
		if err != nil {
			return errors.Wrapf(err, "failed to update summary for epoch %d", epoch)
		}
		if !updated {
			log.Debug().Uint64("epoch", uint64(epoch)).Msg("not enough data to update summary")
			return nil
		}
	}

	return nil
}

func (s *Service) summarizeBlocks(ctx context.Context,
	summaryEpoch phase0.Epoch,
) error {
	if !s.blockSummaries {
		return nil
	}

	md, err := s.getMetadata(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to obtain metadata for block finality")
	}

	lastBlockEpoch := md.LastBlockEpoch
	if lastBlockEpoch != 0 {
		lastBlockEpoch++
	}
	log.Trace().Uint64("last_epoch", uint64(lastBlockEpoch)).Uint64("summary_epoch", uint64(summaryEpoch)).Msg("Blocks catchup bounds")

	// The last epoch updated in the metadata tells us how far we can summarize,
	// as it checks for the component data.  As such, if the finalized epoch
	// is beyond our summarized epoch we truncate to the summarized value.
	// However, if we don't have validator balances the summarizer won't run at all
	// for epochs, so if the last epoch is 0 we continue.
	if summaryEpoch > md.LastEpoch && md.LastEpoch != 0 {
		summaryEpoch = md.LastEpoch
	}

	for epoch := lastBlockEpoch; epoch <= summaryEpoch; epoch++ {
		if err := s.summarizeBlocksInEpoch(ctx, md, epoch); err != nil {
			return errors.Wrap(err, "failed to update block summaries for epoch")
		}
	}

	return nil
}

func (s *Service) summarizeValidators(ctx context.Context, summaryEpoch phase0.Epoch) error {
	if !s.validatorSummaries {
		return nil
	}

	md, err := s.getMetadata(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to obtain metadata for validator summarizer")
	}

	// The last epoch updated in the meadata tells us how far we can summarize,
	// as it checks for the component data.  As such, if the finalized epoch
	// is beyond our summarized epoch we truncate to the summarized value.
	// However, if we don't have validator balances the summarizer won't run at all
	// for epochs, so if the last epoch is 0 we continue.
	if summaryEpoch > md.LastEpoch && md.LastEpoch != 0 {
		summaryEpoch = md.LastEpoch
	}

	lastValidatorEpoch := md.LastValidatorEpoch
	if lastValidatorEpoch != 0 {
		lastValidatorEpoch++
	}

	// Limit the number of epochs summarised per pass, if we are also pruning.
	maxEpochsPerRun := phase0.Epoch(s.maxDaysPerRun) * s.epochsPerDay()
	if s.validatorEpochRetention != nil && maxEpochsPerRun > 0 && summaryEpoch-lastValidatorEpoch > maxEpochsPerRun {
		summaryEpoch = lastValidatorEpoch + maxEpochsPerRun
	}
	log.Trace().Uint64("last_epoch", uint64(lastValidatorEpoch)).Uint64("summary_epoch", uint64(summaryEpoch)).Msg("Validators catchup bounds")

	for epoch := lastValidatorEpoch; epoch <= summaryEpoch; epoch++ {
		if err := s.summarizeValidatorsInEpoch(ctx, md, epoch); err != nil {
			return errors.Wrap(err, fmt.Sprintf("failed to update validator summaries in epoch %d", epoch))
		}
	}

	return nil
}

func (s *Service) summarizeValidatorDays(ctx context.Context) error {
	md, err := s.getMetadata(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to obtain metadata for validator day summarizer")
	}

	epochSummariesTime := s.chainTime.StartOfEpoch(md.LastValidatorEpoch).In(time.UTC)
	daySummariesTime := time.Unix(md.LastValidatorDay, 0).In(time.UTC)
	log.Trace().Time("epoch_summaries_time", epochSummariesTime).Time("day_summaries_time", daySummariesTime).Msg("Times")
	if epochSummariesTime.After(daySummariesTime.AddDate(0, 0, 1)) {
		// We have updates.
		var startTime time.Time
		if md.LastValidatorDay == -1 {
			// Start at the beginning of the day in which genesis occurred.
			genesis := s.chainTime.GenesisTime().In(time.UTC)
			startTime = time.Date(genesis.Year(), genesis.Month(), genesis.Day(), 0, 0, 0, 0, time.UTC)
		} else {
			startTime = daySummariesTime.AddDate(0, 0, 1)
		}
		endTimestamp := epochSummariesTime.AddDate(0, 0, -1)

		for timestamp := startTime; timestamp.Before(endTimestamp); timestamp = timestamp.AddDate(0, 0, 1) {
			if err := s.summarizeValidatorsInDay(ctx, timestamp); err != nil {
				return errors.Wrap(err, fmt.Sprintf("failed to update validator summaries for day %s", timestamp.Format("2006-01-02")))
			}
		}
	}

	return nil
}

func (s *Service) epochsPerDay() phase0.Epoch {
	return phase0.Epoch(86400.0 / s.chainTime.SlotDuration().Seconds() / float64(s.chainTime.SlotsPerEpoch()))
}
