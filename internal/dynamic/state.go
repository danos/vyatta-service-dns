// Copyright (c) 2018-2019, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package dynamic

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/danos/vyatta-service-dns/internal/log"
	"github.com/msoap/byline"
)

type StateData struct {
	Status struct {
		Interfaces []InterfaceStateData `rfc7951:"interfaces"`
	} `rfc7951:"status,omitempty"`
}

type InterfaceStateData struct {
	Name  string          `rfc7951:"name"`
	Hosts []HostStateData `rfc7951:"hosts"`
}

type HostStateData struct {
	IPAddress  string `rfc7951:"address,omitempty"`
	Hostname   string `rfc7951:"hostname"`
	LastUpdate string `rfc7951:"last-update,omitempty"`
	Status     string `rfc7951:"status"`
}

type State struct {
	conf *Config
}

func NewState(conf *Config) *State {
	return &State{conf: conf}
}

func (s *State) Get() *StateData {
	conf := s.conf.Get()
	data := &StateData{}
	for _, intf := range conf.Interface {
		cacheFile := fmt.Sprintf("%s/"+ddclientCacheFmt,
			s.conf.ddclientCacheDir,
			intf.Name)
		f, err := os.Open(cacheFile)
		if err != nil {
			log.Dlog.Println("dns-dynamic-state-get", err)
		}
		idata := readStateData(f, intf.Name)
		data.Status.Interfaces =
			append(data.Status.Interfaces, *idata)
	}
	return data
}

func readStateData(r io.Reader, name string) *InterfaceStateData {
	hosts := make([]map[string]string, 0)
	commentline := regexp.MustCompile("^#")
	byline.NewReader(r).
		SetFS(regexp.MustCompile("[,\\s]+")).
		Grep(func(line []byte) bool {
			return !commentline.Match(line)
		}).
		GrepString(func(line string) bool {
			return line != "\n"
		}).
		AWKMode(
			func(
				line string,
				fields []string,
				vars byline.AWKVars,
			) (out string, err error) {
				vals := make(map[string]string)
				for _, field := range fields {
					split := strings.Split(field, "=")
					if len(split) != 2 {
						continue
					}
					vals[split[0]] = split[1]
				}
				hosts = append(hosts, vals)
				return
			},
		).
		Discard()

	isd := &InterfaceStateData{
		Name:  name,
		Hosts: make([]HostStateData, 0, len(hosts)),
	}
	for _, vals := range hosts {
		vals["status"] = mapStatus(vals["status"])
		out := HostStateData{}
		out.IPAddress = vals["ip"]
		out.Hostname = vals["host"]
		out.Status = vals["status"]

		// Convert UNIX time to RFC3339
		mtime := vals["mtime"]
		t, err := strconv.ParseInt(mtime, 10, 64)
		if err != nil {
			log.Dlog.Println("dns-dynamic-read-state-data:",
				"last-update:",
				err)
		}
		if t != 0 {
			// It is confusing to tell the user the last update was
			// 1970-01-01T00:00:00Z if t == 0
			out.LastUpdate = time.Unix(t, 0).Format(time.RFC3339)
		}
		isd.Hosts = append(isd.Hosts, out)
	}
	return isd
}

func mapStatus(in string) string {
	switch in {
	case "good":
		return "successful"
	case "nochg", "":
		return "nochange"
	case "noconnect":
		return "noconnect"
	case "failed":
		return "failed"
	default:
		log.Dlog.Println("unknown ddclient status", in)
		return "nochange"
	}
}
