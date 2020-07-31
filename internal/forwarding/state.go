// Copyright (c) 2018-2019, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package forwarding

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/danos/vci-service-dns/internal/log"
	"github.com/danos/vci-service-dns/internal/process"
	"github.com/fsnotify/fsnotify"
	"github.com/msoap/byline"
)

type StateData struct {
	State struct {
		QueriesForwarded uint64 `rfc7951:"queries-forwarded"`
		QueriesAnswered  uint64 `rfc7951:"queries-answered"`
		Cache            struct {
			Size          uint32 `rfc7951:"size"`
			Entries       uint64 `rfc7951:"cache-entries"`
			ReusedEntries uint64 `rfc7951:"reused-cache-entries"`
		} `rfc7951:"cache,omitempty"`
		Nameservers []NameserverState `rfc7951:"nameservers,omitempty"`
	} `rfc7951:"state,omitempty"`
}

type NameserverState struct {
	IPAddress              string   `rfc7951:"address"`
	Port                   uint16   `rfc7951:"port"`
	QueriesSent            uint64   `rfc7951:"queries-sent"`
	QueriesRetriedOrFailed uint64   `rfc7951:"queries-retried-or-failed"`
	Provenance             string   `rfc7951:"provenance"`
	InUse                  bool     `rfc7951:"in-use"`
	DomainOverrideOnly     bool     `rfc7951:"domain-override-only"`
	Domains                []string `rfc7951:"domains,omitempty"`
}

type State struct {
	mu         sync.Mutex
	state      atomic.Value
	p          process.Process
	statefile  string
	resolvfile string
	conffile   string
}

func NewState(config *Config) *State {
	s := &State{
		p:          config.forwardingProcess,
		statefile:  config.statefile,
		resolvfile: config.resolvfile,
		conffile:   config.conffile,
	}
	s.state.Store(&StateData{})
	return s
}

func (s *State) Get() *StateData {
	const logPrefix = "forwarding-state-get:"
	// Only one Get at a time can happen since we have to destroy the old file.
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Truncate(s.statefile, 0)
	if err != nil {
		log.Dlog.Println(logPrefix, err)
	}
	watcher := s.prepareWatcher()
	s.requestState()
	gotIt := s.waitForState(watcher)
	if !gotIt {
		// Timeout or error, just return the previous state.
		return s.state.Load().(*StateData)
	}

	stateFile, err := os.Open(s.statefile)
	if err != nil {
		log.Dlog.Println(logPrefix, err)
		return s.state.Load().(*StateData)
	}
	defer stateFile.Close()

	resolvFile, err := os.Open(s.resolvfile)
	if err != nil {
		log.Dlog.Println(logPrefix, err)
	}
	defer resolvFile.Close()

	dnsmasqFile, err := os.Open(s.conffile)
	if err != nil {
		log.Dlog.Println(logPrefix, err)
	}
	defer dnsmasqFile.Close()

	reader := &stateReader{
		dnsmasqStateReader: stateFile,
		resolvConfReader:   resolvFile,
		dnsmasqConfReader:  dnsmasqFile,
	}
	state := reader.Read()
	s.state.Store(state)
	return state
}

func (s *State) requestState() {
	const logPrefix = "forwarding state requester:"
	err := s.p.Signal(syscall.SIGUSR1)
	if err != nil {
		log.Elog.Println(logPrefix, err)
	}

}
func (s *State) prepareWatcher() *fsnotify.Watcher {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}

	watcher.Add(filepath.Dir(s.statefile))
	return watcher
}

func (s *State) waitForState(watcher *fsnotify.Watcher) bool {
	const logPrefix = "forwarding state watcher:"
	defer watcher.Close()
	for {
		select {
		case event := <-watcher.Events:
			switch {
			case event.Op&(fsnotify.CloseWrite|fsnotify.Write) != 0:
				if event.Name != s.statefile {
					continue
				}
				return true
			}
		case err := <-watcher.Errors:
			log.Wlog.Println(logPrefix, err)
		case <-time.After(1 * time.Second):
			log.Wlog.Println(logPrefix, "timeout")
			return false
		}
	}
}

type stateReader struct {
	dnsmasqStateReader io.Reader
	resolvConfReader   io.Reader
	dnsmasqConfReader  io.Reader
}

func (s *stateReader) Read() *StateData {
	//This is a more or less exact port of the old perl state parser.
	//It has been slightly cleaned up, but could probably be cleaner.
	state := s.ReadState()
	s.ReadProvenance(state)
	return state
}

func (s *stateReader) ReadState() *StateData {
	if s.dnsmasqStateReader == nil {
		return &StateData{}
	}
	return readStateData(s.dnsmasqStateReader)
}

func (s *stateReader) ReadProvenance(state *StateData) {
	if s.resolvConfReader == nil || s.dnsmasqConfReader == nil {
		return
	}
	resolvNs := readResolvNs(s.resolvConfReader)
	dhcpNs := readDhclientNs()
	pppNs := readPPPNs()
	dnsmasqNs := readDnsmasqNs(s.dnsmasqConfReader)
	ns := make(map[string]*NameserverState)
	resolvNsM := make(map[string]struct{})

	// By default nothing is in use by dnsmasq unless there are no
	// configured non-domain override servers
	inUse := true
	for _, ns := range dnsmasqNs {
		if ns.Domain == "" {
			inUse = false
			break
		}
	}

	// Find all the nameservers the system knows about and their provenance.
	// This may not be entirely correct but replicates the legacy behavior.
	for _, n := range resolvNs {
		resolvNsM[n] = struct{}{}
		ns[n] = &NameserverState{
			IPAddress:  n,
			Port:       53,
			Provenance: "system",
			InUse:      inUse,
		}
	}

	// Only record dhcpNs that make it into resolv.conf
	for _, n := range dhcpNs {
		_, ok := resolvNsM[n]
		if !ok {
			continue
		}
		ns[n] = &NameserverState{
			IPAddress:  n,
			Port:       53,
			Provenance: "dhcp",
			InUse:      inUse,
		}
	}

	// Only record pppNs that make it into resolv.conf
	for _, n := range pppNs {
		_, ok := resolvNsM[n]
		if !ok {
			continue
		}
		ns[n] = &NameserverState{
			IPAddress:  n,
			Port:       53,
			Provenance: "ppp",
			InUse:      inUse,
		}
	}

	// Adjust the list for the dnsmasq configuration file
	for _, n := range dnsmasqNs {
		s, ok := ns[n.Server]
		if ok {
			s.Provenance = "configuration"
			s.InUse = true
			if n.Domain != "" {
				s.Domains = append(s.Domains, n.Domain)
			} else {
				s.DomainOverrideOnly = false
			}
		} else {
			s := &NameserverState{
				IPAddress:          n.Server,
				Port:               53,
				Provenance:         "configuration",
				InUse:              true,
				DomainOverrideOnly: true,
			}
			if n.Domain != "" {
				s.Domains = []string{n.Domain}
			} else {
				s.DomainOverrideOnly = false
			}
			ns[n.Server] = s
		}
	}

	// Update the list of servers discovered by the state file
	seen := make(map[string]struct{})
	for i, v := range state.State.Nameservers {
		seen[v.IPAddress] = struct{}{}
		s, ok := ns[v.IPAddress]
		if !ok {
			continue
		}
		v.Provenance = s.Provenance
		v.Domains = s.Domains
		v.InUse = s.InUse
		v.DomainOverrideOnly = s.DomainOverrideOnly
		state.State.Nameservers[i] = v
	}

	// Finally add in any servers that weren't in the state file
	for k, v := range ns {
		if _, ok := seen[k]; ok {
			continue
		}
		state.State.Nameservers = append(state.State.Nameservers, *v)
	}
}

func readStateData(r io.Reader) *StateData {
	logPrefix := "read-state-data"
	state := &StateData{}
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return state
	}

	br := bytes.NewReader(data)
	err = readCacheStats(state, br)
	if err != nil {
		log.Dlog.Println(logPrefix, err)
	}
	br.Reset(data)

	err = readQueryStats(state, br)
	if err != nil {
		log.Dlog.Println(logPrefix, err)
	}
	br.Reset(data)

	err = readNameserverStats(state, br)
	if err != nil {
		log.Dlog.Println(logPrefix, err)
	}
	return state
}

func readCacheStats(state *StateData, r io.Reader) error {
	return byline.NewReader(r).
		GrepByRegexp(regexp.MustCompile("cache size")).
		AWKMode(func(line string, fields []string, vars byline.AWKVars) (string, error) {
			if len(fields) < 8 {
				log.Dlog.Println("read-cache-stats",
					errors.New("invalid cache statistics line in log file"))
				return "", nil
			}
			size, err := strconv.ParseUint(strings.Trim(fields[6], ","), 10, 32)
			if err != nil {
				log.Dlog.Println("read-cache-stats:", "cache size:", err)
			}
			state.State.Cache.Size = uint32(size)

			entries := strings.Split(fields[7], "/")
			if len(entries) != 2 {
				log.Dlog.Println("read-cache-stats",
					errors.New("cache entries: unexpected format of entries field"))
				return "", nil
			}
			all, err := strconv.ParseUint(entries[1], 10, 64)
			if err != nil {
				log.Dlog.Println("read-cache-stats:", "cache entries:", err)
			}
			reused, err := strconv.ParseUint(entries[0], 10, 64)
			if err != nil {
				log.Dlog.Println("read-cache-stats:", "cache entries:", err)
			}
			state.State.Cache.Entries = all
			state.State.Cache.ReusedEntries = reused

			return "", nil
		}).
		Discard()

}

func readQueryStats(state *StateData, r io.Reader) error {
	return byline.NewReader(r).
		GrepByRegexp(regexp.MustCompile("queries forwarded")).
		AWKMode(func(line string, fields []string, vars byline.AWKVars) (string, error) {
			if len(fields) < 11 {
				log.Dlog.Println("read-query-stats",
					errors.New("invalid query statistics line in log file"))
				return "", nil
			}
			forwarded, err := strconv.ParseUint(strings.Trim(fields[6], ","), 10, 64)
			if err != nil {
				log.Dlog.Println("read-query-stats:", "forwarded:", err)
			}
			state.State.QueriesForwarded = forwarded

			answered, err := strconv.ParseUint(fields[10], 10, 64)
			if err != nil {
				log.Dlog.Println("read-query-stats:", "answered:", err)
			}
			state.State.QueriesAnswered = answered

			return "", nil
		}).
		Discard()
}

func readNameserverStats(state *StateData, r io.Reader) error {
	var nameservers []NameserverState

	err := byline.NewReader(r).
		GrepByRegexp(regexp.MustCompile("server")).
		AWKMode(func(line string, fields []string, vars byline.AWKVars) (string, error) {
			if len(fields) < 13 {
				log.Dlog.Println("read-nameserver-stats:",
					errors.New("invalid server line in log file: "+line))
				return "", nil
			}

			queriesSent, err := strconv.ParseUint(strings.Trim(fields[8], ","), 10, 64)
			if err != nil {
				log.Dlog.Println("read-nameserver-stats:", "sent:", err)
			}
			queriesRetried, err := strconv.ParseUint(fields[12], 10, 64)
			if err != nil {
				log.Dlog.Println("read-nameserver-stats:", "retried:", err)
			}
			serverPort := strings.Split(strings.Trim(fields[5], ":"), "#")
			var server string
			var port uint64
			if len(serverPort) != 2 {
				log.Dlog.Println("read-nameserver-stats:",
					errors.New("invalid server line in log file: "+line))
				return "", nil
			}
			server = serverPort[0]
			port, err = strconv.ParseUint(serverPort[1], 10, 16)
			if err != nil {
				log.Dlog.Println("read-nameserver-stats:", "port:", err)
			}
			nameservers = append(nameservers, NameserverState{
				IPAddress:              server,
				Port:                   uint16(port),
				QueriesSent:            queriesSent,
				QueriesRetriedOrFailed: queriesRetried,
				Provenance:             "system",
				InUse:                  true,
			})
			return "", nil
		}).
		Discard()
	state.State.Nameservers = nameservers

	return err
}

func readResolvNs(r io.Reader) []string {
	var ns []string
	err := byline.NewReader(r).
		GrepByRegexp(regexp.MustCompile("^nameserver")).
		AWKMode(func(line string, fields []string, vars byline.AWKVars) (string, error) {
			ns = append(ns, fields[1])
			return "", nil
		}).
		Discard()
	if err != nil {
		log.Dlog.Println("read-resolv-nameservers:", err)
	}
	return ns
}

func readAllGlobNs(glob string) []string {
	var out []string
	files, err := filepath.Glob(glob)
	if err != nil {
		log.Dlog.Println("read-all-glob-ns", glob+":", err)
		return nil
	}
	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			log.Dlog.Println("read-all-glob-ns", glob+":", err)
			continue
		}
		out = append(out, readResolvNs(f)...)
		f.Close()
	}
	return out
}

func readDhclientNs() []string {
	const (
		dhclientResolvPat = "/var/lib/dhcp/dhclient-*-resolv.conf"
	)
	return readAllGlobNs(dhclientResolvPat)
}

func readPPPNs() []string {
	const (
		pppResolvPat = "/etc/ppp/resolv-*.conf"
	)
	return readAllGlobNs(pppResolvPat)
}

type dnsMasqNs struct {
	Server string
	Domain string
}

func readDnsmasqNs(r io.Reader) []dnsMasqNs {
	var ns []dnsMasqNs

	serverExp := regexp.MustCompile("server=")
	confDirExp := regexp.MustCompile("conf-dir=")
	err := byline.NewReader(r).
		SetFS(regexp.MustCompile("[=\\s]+")).
		AWKMode(func(line string, fields []string, vars byline.AWKVars) (string, error) {
			switch {
			case serverExp.MatchString(line):
				if fields[3] == "domain-override" {
					domIp := strings.Split(fields[1], "/")
					ns = append(ns, dnsMasqNs{Server: domIp[2], Domain: domIp[1]})
				} else {
					ns = append(ns, dnsMasqNs{Server: fields[1]})
				}
				return "", nil
			case confDirExp.MatchString(line):
				dirGlob := strings.Split(fields[1], ",")
				dir := dirGlob[0]
				globs := dirGlob[1:]
				for _, glob := range globs {
					globpath := dir + "/" + glob
					files, err := filepath.Glob(globpath)
					if err != nil {
						log.Dlog.Println("read-dnsmasq-conf-dir", glob+":", err)
						continue
					}
					for _, file := range files {
						f, err := os.Open(file)
						if err != nil {
							log.Dlog.Println("read-dnsmasq-conf-dir", glob+":", err)
							continue
						}
						ns = append(ns, readDnsmasqNs(f)...)
						f.Close()
					}
				}
			}
			return "", nil
		}).
		Discard()
	if err != nil {
		log.Dlog.Println("read-dnsmasq-nameservers:", err)
	}
	return ns
}
