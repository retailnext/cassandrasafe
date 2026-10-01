// Copyright 2026 RetailNext, Inc.
//
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

package peercheck

import (
	"errors"
	"slices"
	"sync"
)

// progress records which hosts have answered and which tokens are still
// outstanding. It is safe for use from several goroutines.
type progress struct {
	mu         sync.Mutex
	unanswered map[string]struct{}
	tokens     map[string]struct{}
}

func newProgress(tokensByHost map[string][]string) *progress {
	p := &progress{
		unanswered: make(map[string]struct{}, len(tokensByHost)),
		tokens:     make(map[string]struct{}),
	}
	for host, tokens := range tokensByHost {
		p.unanswered[host] = struct{}{}
		for _, token := range tokens {
			p.tokens[token] = struct{}{}
		}
	}
	return p
}

// answered records that host reported tokens. It returns true when no token is
// outstanding after this answer.
func (p *progress) answered(host string, tokens []string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.unanswered, host)
	for _, token := range tokens {
		delete(p.tokens, token)
	}
	return len(p.tokens) == 0
}

// outstanding returns the sorted hosts that have not answered and the number
// of tokens that no host has reported.
func (p *progress) outstanding() ([]string, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	hosts := make([]string, 0, len(p.unanswered))
	for host := range p.unanswered {
		hosts = append(hosts, host)
	}
	slices.Sort(hosts)
	return hosts, len(p.tokens)
}

// result returns nil when every token is accounted for. Otherwise it returns
// an *UnresponsiveHostsError, joined with ctxErr when ctxErr is not nil.
func (p *progress) result(ctxErr error) error {
	hosts, tokens := p.outstanding()
	if tokens == 0 {
		return nil
	}
	err := &UnresponsiveHostsError{Hosts: hosts, OutstandingTokens: tokens}
	if ctxErr != nil {
		return errors.Join(ctxErr, err)
	}
	return err
}
