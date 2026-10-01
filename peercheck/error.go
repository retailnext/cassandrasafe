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

import "fmt"

// UnresponsiveHostsError reports a check that ended with tokens outstanding.
type UnresponsiveHostsError struct {
	// Hosts lists the hosts that did not answer, in sorted order.
	Hosts []string
	// OutstandingTokens counts the tokens that no host reported.
	OutstandingTokens int
}

func (e *UnresponsiveHostsError) Error() string {
	return fmt.Sprintf("%d hosts did not answer and %d tokens are outstanding", len(e.Hosts), e.OutstandingTokens)
}
