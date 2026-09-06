// Command client-request-id-proxy is a small local reverse proxy for testing
// client request ID correlation through protocol-converting proxies.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8787", "local listen address")
	upstream := flag.String("upstream", "", "upstream base URL, for example https://api.example.com")
	fromID := flag.String("from-id", "", "comma-separated client request IDs to rewrite")
	toID := flag.String("to-id", "", "replacement client request ID")
	flag.Parse()

	fromIDs := make(map[string]struct{})
	for _, id := range strings.Split(*fromID, ",") {
		if id = strings.TrimSpace(id); id != "" {
			fromIDs[id] = struct{}{}
		}
	}
	if strings.TrimSpace(*upstream) == "" || len(fromIDs) == 0 || strings.TrimSpace(*toID) == "" {
		flag.Usage()
		log.Fatal("-upstream, -from-id, and -to-id are required")
	}
	target, err := url.Parse(*upstream)
	if err != nil || target.Scheme == "" || target.Host == "" {
		log.Fatalf("invalid -upstream URL: %q", *upstream)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		current := strings.TrimSpace(req.Header.Get("X-Client-Request-ID"))
		if _, ok := fromIDs[current]; ok {
			req.Header.Set("X-Client-Request-ID", strings.TrimSpace(*toID))
			log.Printf("rewrote X-Client-Request-ID for %s %s", req.Method, req.URL.Path)
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		log.Printf("upstream request failed: %s %s: %v", req.Method, req.URL.Path, err)
		http.Error(w, "upstream proxy error", http.StatusBadGateway)
	}

	server := &http.Server{Addr: *listen, Handler: proxy}
	fmt.Printf("listening on http://%s and forwarding to %s\n", *listen, target.String())
	log.Fatal(server.ListenAndServe())
}
