package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRecordQueueTakesAFormPost(t *testing.T) {
	v := url.Values{"u": {"x"}, "current": {"so-3"}, "position": {"1500"}}
	for i := 0; i < 600; i++ {
		v.Add("id", fmt.Sprintf("so-%d", i))
	}
	r := httptest.NewRequest(http.MethodPost, "/rest/savePlayQueue.view", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ParseForm()
	sq := recordQueue(r)
	if sq.Method != "POST" || len(sq.IDs) != 600 || sq.Current != "so-3" || sq.Position != 1500 {
		t.Fatalf("recorded %s, %d ids, %q, %d", sq.Method, len(sq.IDs), sq.Current, sq.Position)
	}
	if lastSaved.IDs[599] != "so-599" {
		t.Fatalf("lastSaved = %v", lastSaved.IDs[len(lastSaved.IDs)-1])
	}
}
