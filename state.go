// Copyright (c) 2018-2019, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package dns

import (
	"github.com/danos/vyatta-service-dns/internal/dynamic"
	"github.com/danos/vyatta-service-dns/internal/forwarding"
)

type StateData struct {
	Service struct {
		DNS *DNSStateData `rfc7951:"vyatta-service-dns-v1:dns,omitempty"`
	} `rfc7951:"vyatta-services-v1:service"`
	Routing *RoutingStateData `rfc7951:"vyatta-routing-v1:routing,omitempty"`
}

type DNSStateData struct {
	Forwarding *forwarding.StateData `rfc7951:"forwarding,omitempty"`
	Dynamic    *dynamic.StateData    `rfc7951:"dynamic,omitempty"`
}

type RoutingStateData struct {
	RoutingInstance []RoutingInstanceData `rfc7951:"routing-instance,omitempty"`
}

type RoutingInstanceData struct {
	Name    string `rfc7951:"instance-name"`
	Service struct {
		DNS struct {
			Forwarding *forwarding.StateData `rfc7951:"forwarding,omitempty"`
			Dynamic    *dynamic.StateData    `rfc7951:"dynamic,omitempty"`
		} `rfc7951:"vyatta-service-dns-routing-instance-v1:dns"`
	} `rfc7951:"service"`
}

type State struct {
	config *Config
}

func StateNew(config *Config) *State {
	return &State{
		config: config,
	}
}

func (s *State) Get() *StateData {
	state := &StateData{}
	ris := make(map[string]*RoutingInstanceData)
	//TODO: we can probably do a lot of this in parallel
	for name, fi := range s.config.getForwardingInstances() {
		if name == "default" {
			if state.Service.DNS == nil {
				state.Service.DNS = &DNSStateData{}
			}
			state.Service.DNS.Forwarding =
				forwarding.NewState(fi).Get()
			continue
		}
		data, ok := ris[name]
		if !ok {
			data = &RoutingInstanceData{
				Name: name,
			}
		}
		data.Service.DNS.Forwarding =
			forwarding.NewState(fi).Get()
		ris[name] = data

	}
	for name, inst := range s.config.getDynamicInstances() {
		if name == "default" {
			if state.Service.DNS == nil {
				state.Service.DNS = &DNSStateData{}
			}
			state.Service.DNS.Dynamic =
				dynamic.NewState(inst).Get()
			continue
		}
		data, ok := ris[name]
		if !ok {
			data = &RoutingInstanceData{
				Name: name,
			}
		}
		data.Service.DNS.Dynamic =
			dynamic.NewState(inst).Get()
		ris[name] = data
	}
	if len(ris) != 0 {
		state.Routing = &RoutingStateData{}
	}
	for _, data := range ris {
		state.Routing.RoutingInstance = append(state.Routing.RoutingInstance, *data)
	}
	return state
}
