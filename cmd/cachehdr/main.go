// Command cachehdr audits Cache-Control headers for a page and its assets.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/PrinceAdvant/cachehdr"
)

func main() {
	max := flag.Int("max", 25, "maximum number of assets to check")
	timeout := flag.Duration("timeout", 15*time.Second, "per-request timeout")
	asJSON := flag.Bool("json", false, "print JSON")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: cachehdr [-max N] [-timeout 15s] [-json] https://example.com/")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	o := cachehdr.Options{Client: &http.Client{Timeout: *timeout}, MaxAssets: *max}
	res, err := cachehdr.Audit(context.Background(), flag.Arg(0), o)
	if err != nil && len(res) == 0 {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	bad := 0
	for _, r := range res {
		if !r.OK {
			bad++
		}
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(res)
	} else {
		for _, r := range res {
			mark := "ok  "
			if !r.OK {
				mark = "FIX "
			}
			cc := r.CacheControl
			if cc == "" {
				cc = "(none)"
			}
			fmt.Printf("%s %-13s %d %s\n     Cache-Control: %s\n", mark, r.Kind, r.Status, r.URL, cc)
			if r.Error != "" {
				fmt.Printf("     error: %s\n", r.Error)
			}
			if r.Advice != "" {
				fmt.Printf("     hint: %s\n", r.Advice)
			}
		}
		fmt.Printf("%d checked, %d to fix\n", len(res), bad)
	}
	if bad > 0 || err != nil {
		os.Exit(1)
	}
}
