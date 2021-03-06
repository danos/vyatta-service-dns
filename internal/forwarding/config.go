// Copyright (c) 2018-2019, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package forwarding

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"text/template"

	"github.com/danos/vyatta-service-dns/internal/fswatcher"
	"github.com/danos/vyatta-service-dns/internal/log"
	"github.com/danos/vyatta-service-dns/internal/process"
)

const cfgFile = `### Autogenerated by vci-service-dns
### Note: Manual changes to this file will be lost during
###       the next commit.
log-facility={{.ForwardingStateFile}}
no-poll
edns-packet-max=4096
{{range .Conf.ListenInterfaces -}}
interface={{.}}
{{end -}}
cache-size={{.Conf.CacheSize}}
{{range .Conf.Nameservers -}}
server={{.}}	# statically configured
{{end -}}
{{range .Conf.DomainOverrides -}}
server=/{{.Domain}}/{{.Server}}	# domain-override
{{end -}}
{{if .UseForwardingConf -}}
resolv-file={{.ForwardingConf}}
{{end -}}
no-hosts
addn-hosts={{.HostsFile}}
conf-dir={{.ConfDir}},{{.ConfDirExt}}
`
const envFile = `### Autogenerated by vyatta-service-dns
DNSMASQ_PID_FILE={{.PidFile}}
DNSMASQ_CONF={{.ConfFile}}
`

var cfgFileTemplate *template.Template
var envFileTemplate *template.Template

func init() {
	t := template.New("ForwardingConf")
	t.Funcs(template.FuncMap{})
	cfgFileTemplate = template.Must(t.Parse(cfgFile))
	t = template.New("ForwardingEnv")
	t.Funcs(template.FuncMap{})
	envFileTemplate = template.Must(t.Parse(envFile))
}

type ConfigData struct {
	DHCPInterfaces   []string `rfc7951:"dhcp,omitempty"`
	CacheSize        uint32   `rfc7951:"cache-size"`
	ListenInterfaces []string `rfc7951:"listen-on,omitempty"`
	Nameservers      []string `rfc7951:"name-server,omitempty"`
	System           bool     `rfc7951:"system,emptyleaf"`

	DomainOverrides []struct {
		Domain string `rfc7951:"tagnode"`
		Server string `rfc7951:"server"`
	} `rfc7951:"domain,omitempty"`
}

func (c *ConfigData) nsDerivedFromConf() bool {
	return len(c.DHCPInterfaces) > 0 || len(c.Nameservers) > 0 || c.System
}

type ConfigOption func(*Config)

func ConfigFile(file string) ConfigOption {
	return func(c *Config) {
		c.conffile = file
	}
}

func ConfigDir(dir string, extensions ...string) ConfigOption {
	return func(c *Config) {
		c.confdir = dir
		c.confdirext = extensions
	}
}

func PIDFile(file string) ConfigOption {
	return func(c *Config) {
		c.pidfile = file
	}
}

func ENVFile(file string) ConfigOption {
	return func(c *Config) {
		c.envfile = file
	}
}

func Unit(unit string) ConfigOption {
	return func(c *Config) {
		c.unit = unit
	}
}

func StateFile(file string) ConfigOption {
	return func(c *Config) {
		c.statefile = file
	}
}

func DHCPConfigFileFmt(pattern string) ConfigOption {
	return func(c *Config) {
		c.dhcpconffilepattern = pattern
	}
}

func DHCPWatchFmt(pattern string) ConfigOption {
	return func(c *Config) {
		c.dhcpwatchpattern = pattern
	}
}

func SystemConfigFile(file string) ConfigOption {
	return func(c *Config) {
		c.systemconffile = file
	}
}

func ResolvFile(file string) ConfigOption {
	return func(c *Config) {
		c.resolvfile = file
	}
}

func HostsFile(file string) ConfigOption {
	return func(c *Config) {
		c.hostsfile = file
	}
}

func InstanceName(name string) ConfigOption {
	return func(c *Config) {
		c.instance = name
	}
}

func VRFHelpers(sub process.VRFSubscriber, chk process.VRFChecker) ConfigOption {
	return func(c *Config) {
		c.vrfSub = sub
		c.vrfChk = chk
		origPcons := c.pCons
		c.pCons = func(name string) process.Process {
			if c.vrfSub != nil {
				return process.NewVrfDependantProcess(
					c.instance,
					c.vrfSub,
					c.vrfChk,
					origPcons(name))
			}
			return origPcons(name)
		}
	}
}

type Config struct {
	currentConfig atomic.Value

	dhcpConfig        *dhcpConfig
	systemConfig      *systemConfig
	resolvWatcher     *reloadWatcher
	hostsWatcher      *reloadWatcher
	forwardingProcess process.Process

	// options
	instance            string
	instanceDir         string
	conffile            string
	confdir             string
	confdirext          []string
	pidfile             string
	envfile             string
	unit                string
	statefile           string
	dhcpwatchpattern    string
	dhcpconffilepattern string
	systemconffile      string
	resolvfile          string
	hostsfile           string
	pCons               func(string) process.Process

	vrfSub process.VRFSubscriber
	vrfChk process.VRFChecker
}

func NewInstanceConfig(name string, opts ...ConfigOption) *Config {
	// Setup a default instance Config object
	instanceDirFmt := "/run/dns/vrf/%s"
	instanceDir := fmt.Sprintf(instanceDirFmt, name)
	iopts := []ConfigOption{
		InstanceName(name),
		Unit(fmt.Sprintf("dnsmasq@%s.service", name)),
		ENVFile(fmt.Sprintf("%s/dnsmasq.env", instanceDir)),

		ConfigFile(fmt.Sprintf("%s/dnsmasq.conf", instanceDir)),
		ConfigDir(fmt.Sprintf("%s/dnsmasq.d", instanceDir), "*.conf"),
		DHCPConfigFileFmt(fmt.Sprintf("%s/dnsmasq.d/dhcpinterface-%%s.conf", instanceDir)),
		SystemConfigFile(fmt.Sprintf("%s/dnsmasq.d/system.conf", instanceDir)),

		PIDFile(fmt.Sprintf("%s/dnsmasq.pid", instanceDir)),
		StateFile(fmt.Sprintf("%s/dnsmasq.log", instanceDir)),

		ResolvFile(fmt.Sprintf("%s/resolv.conf", instanceDir)),
		HostsFile(fmt.Sprintf("%s/hosts", instanceDir)),
	}
	//Allow user options to override default instance options by appending them to the list
	iopts = append(iopts, opts...)
	conf := NewConfig(iopts...)
	conf.instanceDir = instanceDir
	return conf
}

func NewConfig(opts ...ConfigOption) *Config {
	// Default config options
	const (
		conffile             = "/etc/dnsmasq.conf"
		confdir              = "/etc/dnsmasq.d"
		confdirext           = "*.conf"
		unit                 = "dnsmasq.service"
		statefile            = "/var/log/dnsmasq.log"
		pidfile              = "/var/run/dnsmasq/dnsmasq.pid"
		resolvfile           = "/etc/resolv.conf"
		hostsfile            = "/etc/hosts"
		systemNameserverConf = "/etc/dnsmasq.d/system.conf"
		dhcpWatchPattern     = "/var/lib/dhcp/dhclient_%s_lease"
		dhcpConffileTemplate = "/etc/dnsmasq.d/dhcpinterface-%s.conf"
	)

	conf := &Config{
		instance:            "default",
		conffile:            conffile,
		confdir:             confdir,
		confdirext:          []string{confdirext},
		statefile:           statefile,
		pidfile:             pidfile,
		unit:                unit,
		dhcpwatchpattern:    dhcpWatchPattern,
		dhcpconffilepattern: dhcpConffileTemplate,
		systemconffile:      systemNameserverConf,
		resolvfile:          resolvfile,
		hostsfile:           hostsfile,
		pCons:               process.NewSystemdProcess,
	}

	// run options to change the defaults
	for _, opt := range opts {
		opt(conf)
	}

	conf.forwardingProcess = conf.pCons(conf.unit)
	conf.dhcpConfig = &dhcpConfig{
		proc:        conf.forwardingProcess,
		watchFmt:    conf.dhcpwatchpattern,
		confFileFmt: conf.dhcpconffilepattern,
	}
	conf.systemConfig = &systemConfig{
		proc:      conf.forwardingProcess,
		watchFile: conf.resolvfile,
		confFile:  conf.systemconffile,
	}
	conf.currentConfig.Store(&ConfigData{})
	return conf
}

func (c *Config) Get() *ConfigData {
	return c.currentConfig.Load().(*ConfigData)
}

func (c *Config) Set(conf *ConfigData) error {
	const logPrefix = "forwarding-config-set:"
	old := c.Get()
	if reflect.DeepEqual(old, conf) {
		return nil
	}
	if conf != nil {
		err := c.updateConfiguration(conf)
		if err != nil {
			return err
		}
	} else {
		c.deleteConfiguration()
	}
	c.currentConfig.Store(conf)
	return nil
}

func (c *Config) updateConfiguration(conf *ConfigData) error {
	err := c.ensureEnvironment()
	if err != nil {
		return err
	}

	f, err := os.OpenFile(c.conffile,
		os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	err = c.writeForwardingConfig(f, conf)
	if err != nil {
		return err
	}

	if c.envfile != "" {
		envf, err := os.OpenFile(c.envfile,
			os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}
		defer envf.Close()

		err = writeEnvironmentFile(envf, c.pidfile, c.conffile)
		if err != nil {
			return err
		}
	}

	if conf.nsDerivedFromConf() {
		c.resolvWatcher.stop()
		c.resolvWatcher = nil
	} else if c.resolvWatcher == nil {
		c.resolvWatcher = startReloadWatcher(
			c.resolvfile, c.forwardingProcess)
	}

	if c.hostsWatcher == nil {
		c.hostsWatcher = startReloadWatcher(
			c.hostsfile, c.forwardingProcess)
	}

	c.dhcpConfig.Set(conf.DHCPInterfaces)

	c.systemConfig.Set(conf.System)

	err = c.forwardingProcess.Restart()
	if err != nil {
		return err
	}
	return nil
}

func (c *Config) ensureEnvironment() error {
	dirs := []string{c.instanceDir, c.confdir}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) deleteConfiguration() {
	const logPrefix = "forwarding-config-set delete:"
	c.resolvWatcher.stop()
	c.hostsWatcher.stop()
	c.dhcpConfig.Set(nil)
	c.systemConfig.Set(false)
	err := c.forwardingProcess.Stop()
	if err != nil {
		log.Dlog.Println(logPrefix, err)
	}

	files := []string{c.conffile, c.envfile, c.statefile}
	for _, file := range files {
		err = os.Remove(file)
		if err != nil {
			log.Dlog.Println(logPrefix, err)
		}
	}

	if c.instanceDir != "" {
		os.Remove(c.instanceDir)
	}

	if c.confdir != "" {
		err := os.RemoveAll(c.confdir)
		if err != nil {
			log.Dlog.Println(logPrefix, err)
		}
	}
}

func (c *Config) writeForwardingConfig(w io.Writer, conf *ConfigData) error {
	templateInput := struct {
		ConfDir             string
		ConfDirExt          string
		ForwardingConf      string
		ForwardingStateFile string
		Conf                *ConfigData
		UseForwardingConf   bool
		HostsFile           string
	}{
		ConfDir:             c.confdir,
		ConfDirExt:          strings.Join(c.confdirext, ","),
		ForwardingConf:      c.conffile,
		ForwardingStateFile: c.statefile,
		Conf:                conf,
		UseForwardingConf:   conf.nsDerivedFromConf(),
		HostsFile:           c.hostsfile,
	}
	return cfgFileTemplate.Execute(w, &templateInput)
}

func writeEnvironmentFile(w io.Writer, pidfile, conffile string) error {
	tmplInput := struct {
		PidFile, ConfFile string
	}{
		PidFile:  pidfile,
		ConfFile: conffile,
	}
	return envFileTemplate.Execute(w, &tmplInput)
}

type reloadWatcher struct {
	proc    process.Process
	file    string
	watcher *fswatcher.Watcher
}

func startReloadWatcher(
	file string,
	proc process.Process,
) *reloadWatcher {
	out := &reloadWatcher{
		proc: proc,
		file: file,
	}

	out.watcher = fswatcher.Start(
		fswatcher.LogPrefix("resolv watcher:"),
		fswatcher.Logger(log.Dlog),
		fswatcher.Handler(file, out),
	)
	return out
}

func (w *reloadWatcher) CloseWrite(name string) error {
	if name != w.file {
		return nil
	}
	return w.proc.Reload()
}

func (w *reloadWatcher) stop() {
	if w == nil {
		return
	}
	w.watcher.Stop()
}
