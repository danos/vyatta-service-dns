// Copyright (c) 2018-2019, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package dynamic

import (
	"io/ioutil"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danos/vyatta-service-dns/internal/process"
)

func init() {
	// Normalize time processing to UTC so we can test
	// without worring about the timezone in the expected
	// results
	time.Local = time.UTC
}
func TestReadStateData(t *testing.T) {
	var input = `
## ddclient-3.8.3
## last updated at Thu Aug  2 16:44:49 2018 (1533228289)
atime=0,backupmx=0,custom=0,host=test.example.com,ip=10.156.55.202,mtime=1533158251,mx=,script=/nic/update,static=0,status=,warned-min-error-interval=0,warned-min-interval=0,wildcard=0,wtime=30 test.example.com
atime=0,backupmx=0,custom=0,host=test2.example.com,ip=10.156.55.202,mtime=1533158251,mx=,script=/nic/update,static=0,status=,warned-min-error-interval=0,warned-min-interval=0,wildcard=0,wtime=30 test2.example.com
`
	r := strings.NewReader(input)
	data := readStateData(r, "dp0s3")
	expected := &InterfaceStateData{
		Name: "dp0s3",
		Hosts: []HostStateData{
			{
				IPAddress:  "10.156.55.202",
				Hostname:   "test.example.com",
				Status:     "nochange",
				LastUpdate: "2018-08-01T21:17:31Z",
			},
			{
				IPAddress:  "10.156.55.202",
				Hostname:   "test2.example.com",
				Status:     "nochange",
				LastUpdate: "2018-08-01T21:17:31Z",
			},
		},
	}
	if !reflect.DeepEqual(data, expected) {
		t.Log("got", data)
		t.Log("expected", expected)
		t.Fatal("didn't get expected result")
	}
}

func TestReadStateDataZeroTime(t *testing.T) {
	var input = `
## ddclient-3.8.3
## last updated at Wed Aug  1 21:26:10 2018 (1533158770)
atime=0,backupmx=0,custom=0,host=test2.example.com,mtime=0,mx=,script=/nic/update,static=0,status=,warned-min-error-interval=0,warned-min-interval=0,wildcard=0,wtime=30 test2.example.com
`
	r := strings.NewReader(input)
	data := readStateData(r, "dp0s3")
	expected := &InterfaceStateData{
		Name: "dp0s3",
		Hosts: []HostStateData{
			{
				Hostname: "test2.example.com",
				Status:   "nochange",
			},
		},
	}
	if !reflect.DeepEqual(data, expected) {
		t.Log("got", data)
		t.Log("expected", expected)
		t.Fatal("didn't get expected result")
	}
}

func TestStateGet(t *testing.T) {
	defer func() {
		os.RemoveAll("tmp")
	}()
	config := NewConfig(
		DDClientRunDir("tmp/run"),
		DDClientCacheDir("tmp/cache"),
		DDClientConfigDir("tmp/config"),
		DDClientEnvDirFmt("tmp/run/%s"),
	)
	proc := newTproc("tmp/config/ddclient_dp0s3.conf")
	proc2 := newTproc("tmp/config/ddclient_dp0s9.conf")
	config.pCons = func(unit string) process.Process {
		switch unit {
		case "ddclient@dp0s3.service":
			return proc
		case "ddclient@dp0s9.service":
			return proc2
		default:
			t.Fatal("unexpected unit", unit)
			return nil
		}
	}
	cd := &ConfigData{
		Interface: []InterfaceConfigData{
			{
				Name: "dp0s3",
				Service: []ServiceConfigData{
					{
						Name:     "dyndns",
						HostName: []string{"test.example.com"},
						Login:    "user",
						Password: "password",
					},
				},
			},
			{
				Name: "dp0s9",
				Service: []ServiceConfigData{
					{
						Name:     "dyndns",
						HostName: []string{"test2.example.com"},
						Login:    "user",
						Password: "password",
					},
				},
			},
		},
	}
	err := config.Set(cd)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case act := <-proc.actions:
		if act != "reload" {
			t.Fatalf("reload expected, got %s", act)
		}
	case <-time.After(testTimeout):
		t.Fatal("timeout waiting for reload signal")
	}

	select {
	case act := <-proc2.actions:
		if act != "reload" {
			t.Fatalf("reload expected, got %s", act)
		}
	case <-time.After(testTimeout):
		t.Fatal("timeout waiting for reload signal")
	}

	const sample1 = `
## ddclient-3.8.3
## last updated at Thu Aug  2 16:44:49 2018 (1533228289)
atime=0,backupmx=0,custom=0,host=test.example.com,ip=10.156.55.202,mtime=1533158251,mx=,script=/nic/update,static=0,status=,warned-min-error-interval=0,warned-min-interval=0,wildcard=0,wtime=30 test.example.com
`
	const sample2 = `
## ddclient-3.8.3
## last updated at Wed Aug  1 21:26:10 2018 (1533158770)
atime=0,backupmx=0,custom=0,host=test2.example.com,mtime=0,mx=,script=/nic/update,static=0,status=,warned-min-error-interval=0,warned-min-interval=0,wildcard=0,wtime=30 test2.example.com
`
	err = ioutil.WriteFile("tmp/cache/ddclient_dp0s3.cache",
		[]byte(sample1), 0666)
	if err != nil {
		t.Fatal(err)
	}
	err = ioutil.WriteFile("tmp/cache/ddclient_dp0s9.cache",
		[]byte(sample2), 0666)
	if err != nil {
		t.Fatal(err)
	}
	data := NewState(config).Get()
	expected := &StateData{
		Status: struct {
			Interfaces []InterfaceStateData `rfc7951:"interfaces"`
		}{
			Interfaces: []InterfaceStateData{
				{
					Name: "dp0s3",
					Hosts: []HostStateData{
						{
							IPAddress:  "10.156.55.202",
							Hostname:   "test.example.com",
							LastUpdate: "2018-08-01T21:17:31Z",
							Status:     "nochange",
						},
					},
				},
				{
					Name: "dp0s9",
					Hosts: []HostStateData{
						{
							Hostname: "test2.example.com",
							Status:   "nochange",
						},
					},
				},
			},
		},
	}
	if !reflect.DeepEqual(data, expected) {
		t.Log("got", data)
		t.Log("expected", expected)
		t.Fatal("didn't get expected result")
	}
}
