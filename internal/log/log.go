// Copyright (c) 2018-2019, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: MPL-2.0
package log

import (
	"log"
	"log/syslog"
	"os"
)

var (
	Elog *log.Logger
	Dlog *log.Logger
	Ilog *log.Logger
	Wlog *log.Logger
)

func init() {
	// Use syslog if it is available, otherwise fallback
	// to something sensible.
	var err error
	Dlog, err = syslog.NewLogger(syslog.LOG_DEBUG, 0)
	if err != nil {
		Dlog = log.New(os.Stdout, "DEBUG: ", 0)
		Dlog.Println(err)
	}

	Elog, err = syslog.NewLogger(syslog.LOG_ERR, 0)
	if err != nil {
		Elog = log.New(os.Stderr, "ERROR: ", 0)
		Dlog.Println(err)
	}

	Ilog, err = syslog.NewLogger(syslog.LOG_INFO, 0)
	if err != nil {
		Ilog = log.New(os.Stdout, "INFO: ", 0)
		Dlog.Println(err)
	}

	Wlog, err = syslog.NewLogger(syslog.LOG_WARNING, 0)
	if err != nil {
		Wlog = log.New(os.Stderr, "WARNING: ", 0)
		Dlog.Println(err)
	}
}
