// Copyright © 2021 Weald Technology Limited.
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

	eth2client "github.com/attestantio/go-eth2-client"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	zerologger "github.com/rs/zerolog/log"
	"github.com/wealdtech/chaind/services/blocks"
	"github.com/wealdtech/chaind/services/chaindb"
	"github.com/wealdtech/chaind/services/chaintime"
	"golang.org/x/sync/semaphore"
)

// Service is a summarizer service.
type Service struct {
	eth2Client                eth2client.Service
	chainDB                   chaindb.Service
	farFutureEpoch            spec.Epoch
	proposerDutiesProvider    chaindb.ProposerDutiesProvider
	attestationsProvider      chaindb.AttestationsProvider
	blocksProvider            chaindb.BlocksProvider
	blocksSetter              chaindb.BlocksSetter
	depositsProvider          chaindb.DepositsProvider
	validatorsProvider        chaindb.ValidatorsProvider
	attesterSlashingsProvider chaindb.AttesterSlashingsProvider
	proposerSlashingsProvider chaindb.ProposerSlashingsProvider
	chainTime                 chaintime.Service
	blocks                    blocks.Service
	epochSummaries            bool
	blockSummaries            bool
	validatorSummaries        bool
	activitySem               *semaphore.Weighted
}

// module-wide log.
var log zerolog.Logger

// New creates a new service.
func New(ctx context.Context, params ...Parameter) (*Service, error) {
	parameters, err := parseAndCheckParameters(params...)
	if err != nil {
		return nil, errors.Wrap(err, "problem with parameters")
	}

	// Set logging.
	log = zerologger.With().Str("service", "summarizer").Str("impl", "standard").Logger().Level(parameters.logLevel)

	if err := registerMetrics(ctx, parameters.monitor); err != nil {
		return nil, errors.New("failed to register metrics")
	}

	proposerDutiesProvider, isProvider := parameters.chainDB.(chaindb.ProposerDutiesProvider)
	if !isProvider {
		return nil, errors.New("chain DB does not provide proposer duties")
	}

	attestationsProvider, isProvider := parameters.chainDB.(chaindb.AttestationsProvider)
	if !isProvider {
		return nil, errors.New("chain DB does not provide attestations")
	}

	blocksProvider, isProvider := parameters.chainDB.(chaindb.BlocksProvider)
	if !isProvider {
		return nil, errors.New("chain DB does not provide blocks")
	}

	blocksSetter, isSetter := parameters.chainDB.(chaindb.BlocksSetter)
	if !isSetter {
		return nil, errors.New("chain DB does not support block setting")
	}

	depositsProvider, isProvider := parameters.chainDB.(chaindb.DepositsProvider)
	if !isProvider {
		return nil, errors.New("chain DB does not provide deposits")
	}

	validatorsProvider, isProvider := parameters.chainDB.(chaindb.ValidatorsProvider)
	if !isProvider {
		return nil, errors.New("chain DB does not provide validators")
	}

	attesterSlashingsProvider, isProvider := parameters.chainDB.(chaindb.AttesterSlashingsProvider)
	if !isProvider {
		return nil, errors.New("chain DB does not provide attester slashings")
	}

	proposerSlashingsProvider, isProvider := parameters.chainDB.(chaindb.ProposerSlashingsProvider)
	if !isProvider {
		return nil, errors.New("chain DB does not provide proposer slashings")
	}

	s := &Service{
		eth2Client:                parameters.eth2Client,
		chainDB:                   parameters.chainDB,
		farFutureEpoch:            spec.Epoch(0xffffffffffffffff),
		proposerDutiesProvider:    proposerDutiesProvider,
		attestationsProvider:      attestationsProvider,
		blocksProvider:            blocksProvider,
		blocksSetter:              blocksSetter,
		depositsProvider:          depositsProvider,
		validatorsProvider:        validatorsProvider,
		attesterSlashingsProvider: attesterSlashingsProvider,
		proposerSlashingsProvider: proposerSlashingsProvider,
		chainTime:                 parameters.chainTime,
		blocks:                    parameters.blocks,
		epochSummaries:            parameters.epochSummaries,
		blockSummaries:            parameters.blockSummaries,
		validatorSummaries:        parameters.validatorSummaries,
		activitySem:               semaphore.NewWeighted(1),
	}

	return s, nil
}
