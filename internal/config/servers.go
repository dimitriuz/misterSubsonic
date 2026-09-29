package config

import "fmt"

// Clone returns a deep copy (the UI edits a copy and saves it, while the
// connection code keeps the one it started with).
func (c *Config) Clone() *Config {
	d := *c
	d.Servers = append([]Server(nil), c.Servers...)
	return &d
}

// AddServer appends s under a unique name (s.Name, then "s.Name-2", ...)
// and makes it the default. It returns the name used.
func (c *Config) AddServer(s Server) string {
	base, name := s.Name, s.Name
	for n := 2; c.server(name) >= 0; n++ {
		name = fmt.Sprintf("%s-%d", base, n)
	}
	s.Name = name
	c.Servers = append(c.Servers, s)
	c.DefaultServer = name
	return name
}

// RemoveServer deletes the named server. If it was the default, the first
// remaining server becomes the default (none if it was the last).
func (c *Config) RemoveServer(name string) bool {
	i := c.server(name)
	if i < 0 {
		return false
	}
	c.Servers = append(c.Servers[:i:i], c.Servers[i+1:]...)
	if c.DefaultServer == name {
		c.DefaultServer = ""
		if len(c.Servers) > 0 {
			c.DefaultServer = c.Servers[0].Name
		}
	}
	return true
}

func (c *Config) server(name string) int {
	for i, s := range c.Servers {
		if s.Name == name {
			return i
		}
	}
	return -1
}
