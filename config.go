// Copyright (c) 2018-2019, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package dns

import (
	"io/ioutil"
	"sync"
	"sync/atomic"

	"github.com/danos/encoding/rfc7951"
	"github.com/danos/vyatta-service-dns/internal/dynamic"
	"github.com/danos/vyatta-service-dns/internal/forwarding"
	"github.com/danos/vyatta-service-dns/internal/log"
	"github.com/danos/vyatta-service-dns/internal/process"
)

type ConfigData struct {
	Service struct {
		DNS struct {
			Forwarding *forwarding.ConfigData `rfc7951:"forwarding,omitempty"`
			Dynamic    *dynamic.ConfigData    `rfc7951:"dynamic,omitempty"`
		} `rfc7951:"vyatta-service-dns-v1:dns,omitempty"`
	} `rfc7951:"vyatta-services-v1:service,omitempty"`
	Routing struct {
		RoutingInstance []struct {
			Name    string `rfc7951:"instance-name"`
			Service struct {
				DNS struct {
					Forwarding *forwarding.ConfigData `rfc7951:"forwarding,omitempty"`
					Dynamic    *dynamic.ConfigData    `rfc7951:"dynamic,omitempty"`
				} `rfc7951:"vyatta-service-dns-routing-instance-v1:dns,omitempty"`
			} `rfc7951:"service,omitempty"`
		} `rfc7951:"routing-instance,omitempty"`
	} `rfc7951:"vyatta-routing-v1:routing,omitempty"`
}

type ConfigOpt func(*Config)

func Cache(filename string) ConfigOpt {
	return func(c *Config) {
		c.cacheFile = filename
	}
}

func VRFHelpers(sub process.VRFSubscriber, chk process.VRFChecker) ConfigOpt {
	return func(c *Config) {
		c.subscriber = sub
		c.vrfChk = chk
	}
}

func WhenDone(done func()) ConfigOpt {
	return func(c *Config) {
		c.whenDone = done
	}
}

type Config struct {
	writeMu       sync.Mutex
	currentConfig atomic.Value

	dynamicInstances    atomic.Value
	forwardingInstances atomic.Value

	//options
	cacheFile  string
	subscriber process.VRFSubscriber
	vrfChk     process.VRFChecker
	whenDone   func()
}

func ConfigNew(opts ...ConfigOpt) *Config {
	conf := &Config{}
	conf.updateForwardingInstances(make(map[string]*forwarding.Config))
	conf.updateDynamicInstances(make(map[string]*dynamic.Config))
	conf.currentConfig.Store(&ConfigData{})
	for _, opt := range opts {
		opt(conf)
	}
	conf.readCache()
	return conf
}

func (c *Config) Get() *ConfigData {
	return c.currentConfig.Load().(*ConfigData)
}

func (c *Config) Set(newConfig *ConfigData) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.writeCache(newConfig)
	c.syncForwardingInstances(newConfig)
	c.syncDynamicInstances(newConfig)
	if newConfig == nil {
		if c.whenDone != nil {
			c.whenDone()
		}
	}
	return nil
}

func (c *Config) Check(proposedConfig *ConfigData) error {
	// TODO:We could generate all dnsmasq configs and use dnsmasq -C here
	return nil
}

func (c *Config) syncForwardingInstances(newConfig *ConfigData) {
	newFIs := make(map[string]*forwarding.ConfigData)
	if newConfig != nil {
		if newConfig.Service.DNS.Forwarding != nil {
			newFIs["default"] = newConfig.Service.DNS.Forwarding
		}
		for _, ri := range newConfig.Routing.RoutingInstance {
			newFIs[ri.Name] = ri.Service.DNS.Forwarding
		}
	}
	forwardingInstances := c.getForwardingInstances()
	for k, v := range forwardingInstances {
		if _, ok := newFIs[k]; ok {
			continue
		}
		v.Set(nil)
	}
	newFIObjs := make(map[string]*forwarding.Config)
	for k, v := range newFIs {
		conf, ok := forwardingInstances[k]
		if !ok {
			if k == "default" {
				conf = forwarding.NewInstanceConfig(k,
					forwarding.ResolvFile("/etc/resolv.conf"),
					forwarding.HostsFile("/etc/hosts"))
			} else {
				conf = forwarding.NewInstanceConfig(
					k,
					forwarding.VRFHelpers(c.subscriber,
						c.vrfChk),
				)
			}
		}
		conf.Set(v)
		newFIObjs[k] = conf
	}
	c.updateForwardingInstances(newFIObjs)
}

func (c *Config) getForwardingInstances() map[string]*forwarding.Config {
	return c.forwardingInstances.Load().(map[string]*forwarding.Config)
}

func (c *Config) updateForwardingInstances(fis map[string]*forwarding.Config) {
	c.forwardingInstances.Store(fis)
}

func (c *Config) syncDynamicInstances(newConfig *ConfigData) {
	newDIs := make(map[string]*dynamic.ConfigData)
	if newConfig != nil {
		if newConfig.Service.DNS.Dynamic != nil {
			newDIs["default"] = newConfig.Service.DNS.Dynamic
		}
		for _, ri := range newConfig.Routing.RoutingInstance {
			newDIs[ri.Name] = ri.Service.DNS.Dynamic
		}
	}
	instances := c.getDynamicInstances()
	for k, v := range instances {
		if _, ok := newDIs[k]; ok {
			continue
		}
		v.Set(nil)
	}
	newDIObjs := make(map[string]*dynamic.Config)
	for k, v := range newDIs {
		conf, ok := instances[k]
		if !ok {
			if k == "default" {
				conf = dynamic.NewInstanceConfig(k)
			} else {
				conf = dynamic.NewInstanceConfig(
					k,
					dynamic.VRFHelpers(c.subscriber,
						c.vrfChk),
				)
			}
		}
		conf.Set(v)
		newDIObjs[k] = conf
	}
	c.updateDynamicInstances(newDIObjs)

}

func (c *Config) getDynamicInstances() map[string]*dynamic.Config {
	return c.dynamicInstances.Load().(map[string]*dynamic.Config)
}

func (c *Config) updateDynamicInstances(fis map[string]*dynamic.Config) {
	c.dynamicInstances.Store(fis)
}

func (c *Config) readCache() {
	cache := &ConfigData{}
	defer func() {
		c.currentConfig.Store(cache)
	}()
	if c.cacheFile == "" {
		return
	}
	buf, err := ioutil.ReadFile(c.cacheFile)
	if err != nil {
		log.Wlog.Println("read-cache:", err)
		return
	}
	err = rfc7951.Unmarshal(buf, cache)
	if err != nil {
		log.Wlog.Println("read-cache:", err)
		return
	}
	err = c.Set(cache)
	if err != nil {
		log.Elog.Println("read-cache:", err)
	}

}

func (c *Config) writeCache(new *ConfigData) {
	c.currentConfig.Store(new)
	if c.cacheFile == "" {
		return
	}
	buf, err := rfc7951.Marshal(new)
	if err != nil {
		log.Elog.Println("write-cache:", err)
		return
	}
	err = ioutil.WriteFile(c.cacheFile, buf, 0600)
	if err != nil {
		log.Elog.Println("write-cache:", err)
	}
}
