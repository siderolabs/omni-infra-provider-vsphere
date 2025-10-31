// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package config

// Config describes vsphere provider configuration.
type Config struct {
	VSphere VSphereConfig `yaml:"vsphere"`
}

type VSphereConfig struct {
	URI                string `yaml:"uri"`
	User               string `yaml:"user"`
	Password           string `yaml:"password"`
	InsecureSkipVerify bool   `yaml:"insecureSkipVerify"`
}
