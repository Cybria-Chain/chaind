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
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/wealdtech/chaind/services/chaindb"
	"github.com/wealdtech/chaind/services/metrics"
)

type parameters struct {
	logLevel      zerolog.Level
	monitor       metrics.Service
	listenAddress string
	chainDB       chaindb.Service
	timeout       time.Duration
}

// Parameter is the interface for service parameters.
type Parameter interface {
	apply(*parameters)
}

type parameterFunc func(*parameters)

func (f parameterFunc) apply(p *parameters) {
	f(p)
}

// WithLogLevel sets the log level for the module.
func WithLogLevel(logLevel zerolog.Level) Parameter {
	return parameterFunc(func(p *parameters) {
		p.logLevel = logLevel
	})
}

// WithMonitor sets the monitor for the module.
func WithMonitor(monitor metrics.Service) Parameter {
	return parameterFunc(func(p *parameters) {
		p.monitor = monitor
	})
}

// WithChainDB sets the chain database for this module.
func WithChainDB(chainDB chaindb.Service) Parameter {
	return parameterFunc(func(p *parameters) {
		p.chainDB = chainDB
	})
}

// WithListenAddress sets the listen address for this module.
func WithListenAddress(address string) Parameter {
	return parameterFunc(func(p *parameters) {
		p.listenAddress = address
	})
}

// WithTimeout sets the maximum duration for all requests to the endpoint.
func WithTimeout(timeout time.Duration) Parameter {
	return parameterFunc(func(p *parameters) {
		p.timeout = timeout
	})
}

// parseAndCheckParameters parses and checks parameters to ensure that mandatory parameters are present and correct.
func parseAndCheckParameters(params ...Parameter) (*parameters, error) {
	parameters := parameters{
		logLevel: zerolog.GlobalLevel(),
		timeout:  2 * time.Minute,
	}
	for _, p := range params {
		if params != nil {
			p.apply(&parameters)
		}
	}

	if parameters.chainDB == nil {
		return nil, errors.New("no chain database specified")
	}
	if parameters.listenAddress == "" {
		return nil, errors.New("no listen address specified")
	}
	if parameters.timeout == 0 {
		return nil, errors.New("no timeout specified")
	}

	return &parameters, nil
}
