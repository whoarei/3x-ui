package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/mhsanaei/3x-ui/v3/internal/trafficservice"
)

func main() {
	in := flag.String("in", "geosite.dat", "GeoSite database")
	out := flag.String("out", "traffic-services.idx", "compact service index")
	flag.Parse()
	header, err := trafficservice.Generate(*in, *out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"services": len(header.Services), "domainRules": header.Rules, "sourceSHA256": header.SourceHash})
}
