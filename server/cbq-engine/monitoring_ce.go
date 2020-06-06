//  Copyright (c) 2017 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

// +build !enterprise

package main

import (
	"flag"

	"github.com/couchbase/query/clustering"
	"github.com/couchbase/query/errors"
	"github.com/couchbase/query/server"
)

var PROFILE = flag.String("profile", "off", "EE only - Profiling state: off, phases, timings")
var CONTROLS = flag.Bool("controls", false, "EE only - Response to include controls section")

func monitoringInit(configstore clustering.ConfigurationStore) (server.Profile, bool, errors.Error) {
	var err errors.Error

	prof, _ := server.ParseProfile(*PROFILE, false)
	if prof != server.ProfOff {
		err = errors.NewNotImplemented("Profiling is an EE only feature")
	}
	if *CONTROLS {
		err = errors.NewNotImplemented("Controls is an EE only feature")
	}
	return server.ProfOff, false, err
}
