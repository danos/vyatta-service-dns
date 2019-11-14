// Copyright (c) 2018-2019, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"log"
	"os"
	"os/exec"

	"github.com/danos/vci"
	"github.com/danos/vci-service-dns"
	"github.com/msoap/byline"
)

const confFile = "/etc/vci-service-dns.conf"

func init() {
	log.SetFlags(0)
}

type vrfChecker struct {
}

func (vrfChecker) VRFExists(name string) bool {
	cmd := exec.Command("/opt/vyatta/sbin/getvrflist")
	r, err := cmd.StdoutPipe()
	if err != nil {
		log.Println(err)
		return false
	}
	err = cmd.Start()
	if err != nil {
		log.Println(err)
		return false
	}
	var found bool
	err = byline.NewReader(r).
		AWKMode(func(
			_ string, fields []string, _ byline.AWKVars,
		) (out string, err error) {
			if fields[0] == name {
				found = true
			}
			return
		}).
		Discard()
	if err != nil {
		log.Println(err)
	}
	err = cmd.Wait()
	if err != nil {
		log.Println(err)
	}
	return found
}

type vrfSubscriber struct {
	client *vci.Client
}

func (s *vrfSubscriber) SubscribeVRFAdd(handler func(string)) interface {
	Cancel() error
} {
	sub := s.client.Subscribe("vyatta-routing-v1", "instance-added",
		func(in struct {
			Name string `rfc7951:"vyatta-routing-v1:name"`
		}) {
			handler(in.Name)
		})
	err := sub.Run()
	if err != nil {
		log.Println(err)
	}
	return sub
}

func (s *vrfSubscriber) SubscribeVRFDel(handler func(string)) interface {
	Cancel() error
} {
	sub := s.client.Subscribe("vyatta-routing-v1", "instance-removed",
		func(in struct {
			Name string `rfc7951:"vyatta-routing-v1:name"`
		}) {
			handler(in.Name)
		})
	err := sub.Run()
	if err != nil {
		log.Println(err)
	}
	return sub

}

func main() {
	done := make(chan struct{})
	wait := make(chan struct{})

	comp := vci.NewComponent("net.vyatta.vci.dns")

	config := dns.ConfigNew(
		dns.Cache(confFile),
		dns.VRFHelpers(
			&vrfSubscriber{client: comp.Client()},
			vrfChecker{},
		),
		dns.WhenDone(func() { close(done) }),
	)
	state := dns.StateNew(config)
	rpc := dns.RPCNew(config)

	comp.Model("net.vyatta.vci.dns.v1").
		Config(config).
		State(state).
		RPC("vyatta-service-dns-v1", rpc)
	comp.Run()

	go func() {
		comp.Wait()
		close(wait)
	}()

	select {
	case <-done:
	case <-wait:
	}

	log.Println("vci-service-dns", "shutting down")
	os.Exit(0)
}
