package subsonic

import (
	"bytes"
	"encoding/json"
)

// ID is a Subsonic identifier. Servers disagree on whether IDs are JSON
// strings or numbers, so it accepts both.
type ID string

func (id *ID) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*id = ID(s)
		return nil
	}
	if bytes.Equal(b, []byte("null")) {
		*id = ""
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*id = ID(n.String())
	return nil
}

// List decodes a JSON array, and also a lone object, which some servers
// emit when a list has exactly one element.
type List[T any] []T

func (l *List[T]) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '{' {
		var one T
		if err := json.Unmarshal(b, &one); err != nil {
			return err
		}
		*l = List[T]{one}
		return nil
	}
	var many []T
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*l = many
	return nil
}

type Artist struct {
	ID         ID     `json:"id"`
	Name       string `json:"name"`
	CoverArt   ID     `json:"coverArt"`
	AlbumCount int    `json:"albumCount"`
	Starred    string `json:"starred"`
}

type ArtistIndex struct {
	Name    string       `json:"name"`
	Artists List[Artist] `json:"artist"`
}

type ArtistWithAlbums struct {
	Artist
	Albums List[Album] `json:"album"`
}

type Album struct {
	ID        ID     `json:"id"`
	Name      string `json:"name"`
	Artist    string `json:"artist"`
	ArtistID  ID     `json:"artistId"`
	CoverArt  ID     `json:"coverArt"`
	SongCount int    `json:"songCount"`
	Duration  int    `json:"duration"`
	PlayCount int64  `json:"playCount"`
	Year      int    `json:"year"`
	Genre     string `json:"genre"`
	Created   string `json:"created"`
	Starred   string `json:"starred"`
}

type AlbumWithSongs struct {
	Album
	Songs List[Song] `json:"song"`
}

// ReplayGain is the OpenSubsonic replayGain object (values in dB / linear peak).
type ReplayGain struct {
	TrackGain *float64 `json:"trackGain"`
	AlbumGain *float64 `json:"albumGain"`
	TrackPeak *float64 `json:"trackPeak"`
	AlbumPeak *float64 `json:"albumPeak"`
}

type Song struct {
	ID           ID          `json:"id"`
	Parent       ID          `json:"parent"`
	Title        string      `json:"title"`
	Album        string      `json:"album"`
	Artist       string      `json:"artist"`
	AlbumID      ID          `json:"albumId"`
	ArtistID     ID          `json:"artistId"`
	CoverArt     ID          `json:"coverArt"`
	Track        int         `json:"track"`
	DiscNumber   int         `json:"discNumber"`
	Year         int         `json:"year"`
	Genre        string      `json:"genre"`
	Size         int64       `json:"size"`
	ContentType  string      `json:"contentType"`
	Suffix       string      `json:"suffix"`
	Duration     int         `json:"duration"` // seconds
	BitRate      int         `json:"bitRate"`  // kbps
	BitDepth     int         `json:"bitDepth"`
	SamplingRate int         `json:"samplingRate"`
	ChannelCount int         `json:"channelCount"`
	Starred      string      `json:"starred"`
	ReplayGain   *ReplayGain `json:"replayGain"`
}

type Genre struct {
	Name       string `json:"value"`
	SongCount  int    `json:"songCount"`
	AlbumCount int    `json:"albumCount"`
}

type Playlist struct {
	ID        ID     `json:"id"`
	Name      string `json:"name"`
	Owner     string `json:"owner"`
	Public    bool   `json:"public"`
	SongCount int    `json:"songCount"`
	Duration  int    `json:"duration"`
	CoverArt  ID     `json:"coverArt"`
	Comment   string `json:"comment"`
}

type PlaylistWithSongs struct {
	Playlist
	Songs List[Song] `json:"entry"`
}

type Starred struct {
	Artists List[Artist] `json:"artist"`
	Albums  List[Album]  `json:"album"`
	Songs   List[Song]   `json:"song"`
}

type SearchResult struct {
	Artists List[Artist] `json:"artist"`
	Albums  List[Album]  `json:"album"`
	Songs   List[Song]   `json:"song"`
}

type PlayQueue struct {
	Songs    List[Song] `json:"entry"`
	Current  ID         `json:"current"`
	Position int64      `json:"position"` // milliseconds
	Changed  string     `json:"changed"`
}

type Extension struct {
	Name     string `json:"name"`
	Versions []int  `json:"versions"`
}

// ServerInfo is what Connect learned about the server.
type ServerInfo struct {
	APIVersion    string
	Type          string // "navidrome", "gonic", ... (OpenSubsonic)
	ServerVersion string
	OpenSubsonic  bool
	Extensions    map[string][]int
}

func (s *ServerInfo) HasExtension(name string) bool {
	_, ok := s.Extensions[name]
	return ok
}

// response is the payload inside "subsonic-response".
type response struct {
	Status        string    `json:"status"`
	Version       string    `json:"version"`
	Type          string    `json:"type"`
	ServerVersion string    `json:"serverVersion"`
	OpenSubsonic  bool      `json:"openSubsonic"`
	Error         *APIError `json:"error"`

	Artists *struct {
		Index List[ArtistIndex] `json:"index"`
	} `json:"artists"`
	Artist     *ArtistWithAlbums `json:"artist"`
	Album      *AlbumWithSongs   `json:"album"`
	Song       *Song             `json:"song"`
	AlbumList2 *struct {
		Albums List[Album] `json:"album"`
	} `json:"albumList2"`
	Genres *struct {
		Genres List[Genre] `json:"genre"`
	} `json:"genres"`
	Playlists *struct {
		Playlists List[Playlist] `json:"playlist"`
	} `json:"playlists"`
	Playlist      *PlaylistWithSongs `json:"playlist"`
	Starred2      *Starred           `json:"starred2"`
	SearchResult3 *SearchResult      `json:"searchResult3"`
	PlayQueue     *PlayQueue         `json:"playQueue"`
	Extensions    List[Extension]    `json:"openSubsonicExtensions"`
}

func (s Song) IsStarred() bool   { return s.Starred != "" }
func (a Album) IsStarred() bool  { return a.Starred != "" }
func (a Artist) IsStarred() bool { return a.Starred != "" }
