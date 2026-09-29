//go:build !linux

package platform

import "errors"

// Console is a console switched to graphics mode (Linux only).
type Console struct{}

// GraphicsMode needs a Linux virtual terminal.
func GraphicsMode() (*Console, error) { return nil, errors.New("platform: no Linux console") }

// Restore does nothing here.
func (c *Console) Restore() error { return nil }

// RestoreText needs a Linux virtual terminal.
func RestoreText() error { return errors.New("platform: no Linux console") }
