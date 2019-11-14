// Copyright (c) 2018-2019, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package forwarding

import (
	"syscall"
)

type RPC struct {
	conf *Config
}

func RPCNew(config *Config) *RPC {
	return &RPC{conf: config}
}

func (r *RPC) ResetDnsForwarding() (struct{}, error) {
	err := r.conf.forwardingProcess.Restart()
	return struct{}{}, err
}

func (r *RPC) ResetDnsForwardingCache() (struct{}, error) {
	err := r.conf.forwardingProcess.Signal(syscall.SIGHUP)
	return struct{}{}, err
}
