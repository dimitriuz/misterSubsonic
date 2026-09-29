package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const examplePath = "../../sdcard/mistersubsonic/config.example.toml"

// The shipped example loads cleanly and shows the defaults.
func TestExampleConfigLoads(t *testing.T) {
	c, warns, err := Load(examplePath)
	if err != nil || len(warns) != 0 {
		t.Fatalf("err %v, warnings %v", err, warns)
	}
	d := Default()
	if c.Playback != d.Playback || c.Display != d.Display || c.Cache != d.Cache {
		t.Fatalf("the example's settings differ from the defaults:\n%+v %+v %+v\n%+v %+v %+v",
			c.Playback, c.Display, c.Cache, d.Playback, d.Display, d.Cache)
	}
	if s, ok := c.ActiveServer(); !ok || s.Name != "home" {
		t.Fatalf("server %+v", s)
	}
}

// Every option is in the example, at least as a comment.
func TestExampleConfigShowsEveryOption(t *testing.T) {
	b, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, typ := range []reflect.Type{reflect.TypeOf(Config{}), reflect.TypeOf(Server{}), reflect.TypeOf(Playback{}),
		reflect.TypeOf(Display{}), reflect.TypeOf(Cache{})} {
		for i := range typ.NumField() {
			key := strings.Split(typ.Field(i).Tag.Get("toml"), ",")[0]
			if typ.Field(i).Type.Kind() == reflect.Struct || typ.Field(i).Type.Kind() == reflect.Slice {
				continue // tables: [playback], [[server]]
			}
			if !strings.Contains(text, key+" = ") {
				t.Errorf("%s isn't in the example", key)
			}
		}
	}
}
