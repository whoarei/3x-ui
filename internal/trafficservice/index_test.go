package trafficservice

import (
	"os"
	"path/filepath"
	"testing"

	routercommon "github.com/xtls/xray-core/common/geodata"
	"google.golang.org/protobuf/proto"
)

func TestServiceIndexPrefersSpecificServicesAndCustomOverrides(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "geosite.dat")
	destination := filepath.Join(dir, "services.idx")
	domain := func(value string) *routercommon.Domain {
		return &routercommon.Domain{Type: routercommon.Domain_Domain, Value: value}
	}
	data, err := proto.Marshal(&routercommon.GeoSiteList{Entry: []*routercommon.GeoSite{
		{Code: "GOOGLE", Domain: []*routercommon.Domain{domain("google.com"), domain("youtube.com")}},
		{Code: "YOUTUBE", Domain: []*routercommon.Domain{domain("youtube.com")}},
		{Code: "CATEGORY-TECH", Domain: []*routercommon.Domain{domain("example.org")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(source, destination); err != nil {
		t.Fatal(err)
	}
	index, err := Load(destination, "")
	if err != nil {
		t.Fatal(err)
	}
	for host, want := range map[string]string{"www.youtube.com": "youtube", "www.google.com": "google", "news.example.org": "domain:example.org", "__unknown__": "unknown"} {
		if got := index.Match(host); got != want {
			t.Fatalf("%s: got %s, want %s", host, got, want)
		}
	}
	override := filepath.Join(dir, "overrides.json")
	if err := os.WriteFile(override, []byte(`{"GOOGLE.COM":"Work"}`), 0600); err != nil {
		t.Fatal(err)
	}
	index, err = Load(destination, override)
	if err != nil {
		t.Fatal(err)
	}
	if got := index.Match("www.google.com"); got != "custom:Work" {
		t.Fatalf("parent override did not override built-in match: %s", got)
	}
}
