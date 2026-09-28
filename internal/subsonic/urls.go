package subsonic

import (
	"net/url"
	"strconv"
)

type StreamOptions struct {
	Format     string // "raw" for the original file, or a transcode target like "mp3"
	MaxBitRate int    // kbps, 0 = server default
	TimeOffset int    // seconds; only honoured for transcoded streams
}

func (c *Client) StreamURL(id ID, o StreamOptions) string {
	v := url.Values{"id": {string(id)}}
	if o.Format != "" {
		v.Set("format", o.Format)
	}
	if o.MaxBitRate > 0 {
		v.Set("maxBitRate", strconv.Itoa(o.MaxBitRate))
	}
	if o.TimeOffset > 0 {
		v.Set("timeOffset", strconv.Itoa(o.TimeOffset))
	}
	return c.endpointURL("stream", v)
}

func (c *Client) CoverArtURL(id ID, size int) string {
	v := url.Values{"id": {string(id)}}
	if size > 0 {
		v.Set("size", strconv.Itoa(size))
	}
	return c.endpointURL("getCoverArt", v)
}
