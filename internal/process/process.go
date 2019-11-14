// Copyright (c) 2018-2019, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: MPL-2.0
package process

import (
	"sync"
	"syscall"

	systemd "github.com/coreos/go-systemd/dbus"
)

type Process interface {
	Start() error
	Stop() error
	Reload() error
	Restart() error
	Signal(syscall.Signal) error
}

type SystemdProcess struct {
	unit string
}

func NewSystemdProcess(unit string) Process {
	return &SystemdProcess{
		unit: unit,
	}
}

func (p *SystemdProcess) Start() error {
	conn, err := systemd.NewSystemdConnection()
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.StartUnit(p.unit, "replace", nil)
	return err
}

func (p *SystemdProcess) Stop() error {
	conn, err := systemd.NewSystemdConnection()
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.StopUnit(p.unit, "replace", nil)

	return err
}

func (p *SystemdProcess) Reload() error {
	conn, err := systemd.NewSystemdConnection()
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.ReloadOrRestartUnit(p.unit, "replace", nil)
	return err
}

func (p *SystemdProcess) Restart() error {
	conn, err := systemd.NewSystemdConnection()
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.RestartUnit(p.unit, "replace", nil)
	return err
}

func (p *SystemdProcess) Signal(signal syscall.Signal) error {
	conn, err := systemd.NewSystemdConnection()
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.KillUnit(p.unit, int32(signal))
	return nil
}

type VrfDependantProcess struct {
	vrf  string
	proc Process
	chk  VRFChecker

	action chan act
	vrfAdd chan struct{}
	vrfDel chan struct{}
	done   chan struct{}

	subs []interface{ Cancel() error }
}

type VRFSubscriber interface {
	SubscribeVRFAdd(func(string)) interface {
		Cancel() error
	}
	SubscribeVRFDel(func(string)) interface {
		Cancel() error
	}
}

type VRFChecker interface {
	VRFExists(name string) bool
}

func NewVrfDependantProcess(
	vrf string,
	sub VRFSubscriber,
	chk VRFChecker,
	proc Process,
) Process {
	out := &VrfDependantProcess{
		vrf:    vrf,
		proc:   proc,
		chk:    chk,
		action: make(chan act, 1),
		vrfAdd: make(chan struct{}, 1),
		vrfDel: make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
	out.subs = append(out.subs,
		sub.SubscribeVRFAdd(func(name string) {
			if name != vrf {
				return
			}
			out.vrfAdded()
		}))
	out.subs = append(out.subs,
		sub.SubscribeVRFDel(func(name string) {
			if name != vrf {
				return
			}
			out.vrfDeleted()
		}))
	var wg sync.WaitGroup
	wg.Add(1)
	go out.run(&wg)
	wg.Wait()
	return out
}

func (p *VrfDependantProcess) Start() error {
	ret := make(chan error, 1)
	p.action <- act{ret: ret, action: p.proc.Start}
	return <-ret
}
func (p *VrfDependantProcess) Stop() error {
	ret := make(chan error, 1)
	for _, sub := range p.subs {
		sub.Cancel()
	}
	p.action <- act{ret: ret, action: p.proc.Stop}
	err := <-ret
	close(p.done)
	return err
}
func (p *VrfDependantProcess) Reload() error {
	ret := make(chan error, 1)
	p.action <- act{ret: ret, action: p.proc.Reload}
	return <-ret
}
func (p *VrfDependantProcess) Restart() error {
	ret := make(chan error, 1)
	p.action <- act{ret: ret, action: p.proc.Restart}
	return <-ret
}
func (p *VrfDependantProcess) Signal(sig syscall.Signal) error {
	ret := make(chan error, 1)
	p.action <- act{ret: ret, action: func() error {
		return p.proc.Signal(sig)
	}}
	return <-ret
}

func (p *VrfDependantProcess) run(wg *sync.WaitGroup) {
	var vrfSeen bool
	var action func() error

	vrfSeen = p.vrfExists()
	wg.Done()
	for {
		select {
		case <-p.vrfAdd:
			vrfSeen = true
			if action != nil {
				action()
			}
		case <-p.vrfDel:
			vrfSeen = false
		case act := <-p.action:
			var err error
			action = act.action
			if vrfSeen {
				err = action()
			}
			if act.ret != nil {
				act.ret <- err
			}
		case <-p.done:
			return
		}
	}
}

func (p *VrfDependantProcess) vrfAdded() {
	p.vrfAdd <- struct{}{}
}

func (p *VrfDependantProcess) vrfDeleted() {
	p.vrfDel <- struct{}{}
}

func (p *VrfDependantProcess) vrfExists() bool {
	if p.chk == nil {
		return false
	}
	return p.chk.VRFExists(p.vrf)
}

type act struct {
	ret    chan error
	action func() error
}
