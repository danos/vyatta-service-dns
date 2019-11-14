// Copyright (c) 2018-2019, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package forwarding

import (
	"reflect"
	"strings"
	"testing"
)

func TestReadStateData(t *testing.T) {
	const sample = `
Jul 23 11:57:35 dnsmasq[28935]: time 1532372255
Jul 23 11:57:35 dnsmasq[28935]: cache size 150, 83961/1213146 cache insertions re-used unexpired cache entries.
Jul 23 11:57:35 dnsmasq[28935]: queries forwarded 363690, queries answered locally 229001
Jul 23 11:57:35 dnsmasq[28935]: queries for authoritative zones 0
Jul 23 11:57:35 dnsmasq[28935]: server 172.22.102.1#53: queries sent 0, retried or failed 0
Jul 23 11:57:35 dnsmasq[28935]: server 172.22.20.4#53: queries sent 15524, retried or failed 412
Jul 23 11:57:35 dnsmasq[28935]: server 208.201.224.33#53: queries sent 293857, retried or failed 606
Jul 23 11:57:35 dnsmasq[28935]: server 208.201.224.11#53: queries sent 114783, retried or failed 861
`
	expected := &StateData{}
	expected.State.QueriesForwarded = 363690
	expected.State.QueriesAnswered = 229001
	expected.State.Cache.Size = 150
	expected.State.Cache.Entries = 1213146
	expected.State.Cache.ReusedEntries = 83961
	expected.State.Nameservers = []NameserverState{
		{
			IPAddress:              "172.22.102.1",
			Port:                   53,
			QueriesSent:            0,
			QueriesRetriedOrFailed: 0,
			Provenance:             "system",
			InUse:                  true,
		},
		{
			IPAddress:              "172.22.20.4",
			Port:                   53,
			QueriesSent:            15524,
			QueriesRetriedOrFailed: 412,
			Provenance:             "system",
			InUse:                  true,
		},
		{
			IPAddress:              "208.201.224.33",
			Port:                   53,
			QueriesSent:            293857,
			QueriesRetriedOrFailed: 606,
			Provenance:             "system",
			InUse:                  true,
		},
		{
			IPAddress:              "208.201.224.11",
			Port:                   53,
			QueriesSent:            114783,
			QueriesRetriedOrFailed: 861,
			Provenance:             "system",
			InUse:                  true,
		},
	}
	r := strings.NewReader(sample)
	data := readStateData(r)
	if !reflect.DeepEqual(data, expected) {
		t.Log("got", data)
		t.Log("expected", expected)
		t.Fatal("didn't get expected value")
	}
}

func TestReadStateDataInvalidReturnsWhatCanBeParsed(t *testing.T) {
	const sample = `
Jul 23 11:57:35 dnsmasq[28935]: time 1532372255
Jul 23 11:57:35 dnsmasq[28935]: cache size 150a, 83961b/1213146c cache insertions re-used unexpired cache entries.
Jul 23 11:57:35 dnsmasq[28935]: queries forwarded 363690e, queries answered locally 229001f
Jul 23 11:57:35 dnsmasq[28935]: queries for authoritative zones 0
Jul 23 11:57:35 dnsmasq[28935]: server 172.22.102.1#53: queries sent 0g, retried or failed 0
Jul 23 11:57:35 dnsmasq[28935]: server 172.22.20.4#53: queries sent 15524, retried or failed 412h
Jul 23 11:57:35 dnsmasq[28935]: server 208.201.224.33#53i: queries sent 293857, retried or failed 606
Jul 23 11:57:35 dnsmasq[28935]: server 208.201.224.11#baz#53: queries sent 114783, retried or failed 861
`
	expected := &StateData{}
	expected.State.QueriesForwarded = 0
	expected.State.QueriesAnswered = 0
	expected.State.Cache.Size = 0
	expected.State.Cache.Entries = 0
	expected.State.Cache.ReusedEntries = 0
	expected.State.Nameservers = []NameserverState{
		{
			IPAddress:              "172.22.102.1",
			Port:                   53,
			QueriesSent:            0,
			QueriesRetriedOrFailed: 0,
			Provenance:             "system",
			InUse:                  true,
		},
		{
			IPAddress:              "172.22.20.4",
			Port:                   53,
			QueriesSent:            15524,
			QueriesRetriedOrFailed: 0,
			Provenance:             "system",
			InUse:                  true,
		},
		{
			IPAddress:              "208.201.224.33",
			Port:                   0,
			QueriesSent:            293857,
			QueriesRetriedOrFailed: 606,
			Provenance:             "system",
			InUse:                  true,
		},
	}
	r := strings.NewReader(sample)
	data := readStateData(r)
	if !reflect.DeepEqual(data, expected) {
		t.Log("got", data)
		t.Log("expected", expected)
		t.Fatal("didn't get expected value")
	}
}

func TestReadStateDataWithProvenance(t *testing.T) {
	const sample = `
Jul 23 11:57:35 dnsmasq[28935]: time 1532372255
Jul 23 11:57:35 dnsmasq[28935]: cache size 150, 83961/1213146 cache insertions re-used unexpired cache entries.
Jul 23 11:57:35 dnsmasq[28935]: queries forwarded 363690, queries answered locally 229001
Jul 23 11:57:35 dnsmasq[28935]: queries for authoritative zones 0
Jul 23 11:57:35 dnsmasq[28935]: server 172.22.102.1#53: queries sent 0, retried or failed 0
Jul 23 11:57:35 dnsmasq[28935]: server 172.22.20.4#53: queries sent 15524, retried or failed 412
Jul 23 11:57:35 dnsmasq[28935]: server 208.201.224.33#53: queries sent 293857, retried or failed 606
Jul 23 11:57:35 dnsmasq[28935]: server 208.201.224.11#53: queries sent 114783, retried or failed 861
`
	const dnsmasqConf = `
#
# autogenerated by vyatta-dns-forwarding.pl on Thu Jun 28 09:50:05 PDT 2018
#
no-poll
edns-packet-max=4096
interface=br0
interface=vtun3
interface=vtun5
interface=ppp0
interface=ppp1
interface=ppp2
interface=ppp3
cache-size=150
server=208.201.224.11	# system
server=208.201.224.33	# system
server=/att.com/172.22.20.4	# domain-override
server=/eng.vyatta.net/172.22.20.4	# domain-override
server=/paul.jsouthworth.net/172.22.102.1	# domain-override
log-facility=/var/log/dnsmasq.log
resolv-file=/etc/dnsmasq.conf
no-hosts
addn-hosts=etc/hosts
`

	const resolvConf = `
# file generated by /opt/vyatta/sbin/vyatta_update_resolv.pl do not edit
domain		straylight.jsouthworth.net
nameserver	208.201.224.11
nameserver	208.201.224.33
`
	expected := &StateData{}
	expected.State.QueriesForwarded = 363690
	expected.State.QueriesAnswered = 229001
	expected.State.Cache.Size = 150
	expected.State.Cache.Entries = 1213146
	expected.State.Cache.ReusedEntries = 83961
	expected.State.Nameservers = []NameserverState{
		{
			IPAddress:              "172.22.102.1",
			Port:                   53,
			QueriesSent:            0,
			QueriesRetriedOrFailed: 0,
			Provenance:             "configuration",
			InUse:                  true,
			DomainOverrideOnly:     true,
			Domains: []string{
				"paul.jsouthworth.net",
			},
		},
		{
			IPAddress:              "172.22.20.4",
			Port:                   53,
			QueriesSent:            15524,
			QueriesRetriedOrFailed: 412,
			Provenance:             "configuration",
			InUse:                  true,
			DomainOverrideOnly:     true,
			Domains: []string{
				"att.com",
				"eng.vyatta.net",
			},
		},
		{
			IPAddress:              "208.201.224.33",
			Port:                   53,
			QueriesSent:            293857,
			QueriesRetriedOrFailed: 606,
			Provenance:             "configuration",
			InUse:                  true,
		},
		{
			IPAddress:              "208.201.224.11",
			Port:                   53,
			QueriesSent:            114783,
			QueriesRetriedOrFailed: 861,
			Provenance:             "configuration",
			InUse:                  true,
		},
	}
	r := strings.NewReader(sample)
	dr := strings.NewReader(dnsmasqConf)
	rr := strings.NewReader(resolvConf)
	sr := &stateReader{
		dnsmasqStateReader: r,
		dnsmasqConfReader:  dr,
		resolvConfReader:   rr,
	}
	data := sr.Read()
	if !reflect.DeepEqual(data, expected) {
		t.Log("got", data)
		t.Log("expected", expected)
		t.Fatal("didn't get expected value")
	}
}
