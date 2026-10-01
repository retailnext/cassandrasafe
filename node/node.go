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

// Package node holds the data types that describe a Cassandra node and its
// peers. It has no logic.
package node

// Local describes the node that answered a system.local query.
type Local struct {
	BootstrapState string
	ClusterName    string
	DataCenter     string
	HostID         string
	Partitioner    string
	Rack           string
	Tokens         []string
}

// Peer describes one row of system.peers.
// Address is the peer column. It holds the broadcast address of the peer.
type Peer struct {
	Address        string
	DataCenter     string
	HostID         string
	PreferredIP    string
	Rack           string
	ReleaseVersion string
	RPCAddress     string
	SchemaVersion  string
	Tokens         []string
}

// TokensByHost maps a peer address to the tokens that system.peers lists for
// that peer.
type TokensByHost map[string][]string
