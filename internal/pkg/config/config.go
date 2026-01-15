// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package config

import "fmt"

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

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.VSphere.URI == "" {
		return fmt.Errorf("vsphere.uri is required")
	}

	if c.VSphere.User == "" {
		return fmt.Errorf("vsphere.user is required")
	}

	if c.VSphere.Password == "" {
		return fmt.Errorf("vsphere.password is required")
	}

	return nil
}
