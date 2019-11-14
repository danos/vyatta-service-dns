// Copyright (c) 2018-2019, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package dns

import (
	"github.com/danos/vci-service-dns/internal/dynamic"
	"github.com/danos/vci-service-dns/internal/forwarding"
	"github.com/danos/mgmterror"
)

type RPC struct {
	conf *Config
}

func RPCNew(conf *Config) *RPC {
	return &RPC{
		conf: conf,
	}
}

func (r *RPC) ResetDnsForwarding(in struct {
	RoutingInstance string `rfc7951:"vyatta-service-dns-routing-instance-v1:routing-instance"`
}) (struct{}, error) {
	if in.RoutingInstance == "" {
		in.RoutingInstance = "default"
	}
	fis := r.conf.getForwardingInstances()

	c, ok := fis[in.RoutingInstance]
	if !ok {
		err := mgmterror.NewMustViolationError()
		err.Path = "/routing-instance/" + in.RoutingInstance
		err.Message = "DNS forwarding is not configured on requested instance"
		return struct{}{}, err
	}
	return forwarding.RPCNew(c).ResetDnsForwarding()
}

func (r *RPC) ResetDnsForwardingCache(in struct {
	RoutingInstance string `rfc7951:"vyatta-service-dns-routing-instance-v1:routing-instance"`
}) (struct{}, error) {
	if in.RoutingInstance == "" {
		in.RoutingInstance = "default"
	}
	fis := r.conf.getForwardingInstances()
	c := fis[in.RoutingInstance]
	c, ok := fis[in.RoutingInstance]
	if !ok {
		err := mgmterror.NewMustViolationError()
		err.Path = "/routing-instance/" + in.RoutingInstance
		err.Message = "DNS forwarding is not configured on requested instance"
		return struct{}{}, err
	}
	return forwarding.RPCNew(c).ResetDnsForwardingCache()
}

func (r *RPC) UpdateDynamicDnsInterface(
	in struct {
		Interface string `rfc7951:"vyatta-service-dns-v1:interface"`
	},
) (struct{}, error) {
	dis := r.conf.getDynamicInstances()
	for _, conf := range dis {
		conf := conf.Get()
		for _, intf := range conf.Interface {
			if intf.Name != in.Interface {
				continue
			}
			return dynamic.UpdateDynamicDnsInterface(intf.Name)
		}
	}
	err := mgmterror.NewMustViolationError()
	err.Path = "/interface/" + in.Interface
	err.Message = "There is no dynamic DNS instance running on the specified interface"
	return struct{}{}, err
}
