// Copyright (c) 2018-2019, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package dynamic

import (
	"fmt"

	"github.com/danos/vci-service-dns/internal/process"
)

func UpdateDynamicDnsInterface(intf string) (struct{}, error) {
	proc := process.NewSystemdProcess(fmt.Sprintf(ddclientUnitFmt, intf))
	err := proc.Restart()
	return struct{}{}, err
}
