package connector

import "slices"

// ConfigCommands returns a copy of the exact running-configuration sequence.
func (d *Device) ConfigCommands() []string { return slices.Clone(d.profile.ConfigCommands) }

// SaveCommands returns a copy of the profile's persistence sequence.
func (d *Device) SaveCommands() []string { return slices.Clone(d.profile.SaveCommands) }
