package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"socks5-server-ng/pkg/go-socks5"
)

var template = `
<html>
	<body>
		<a href="/metrics">Metrics</a>
	</body>
</html>
`

func serveStatusPage(server *socks5.Server, addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(template))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		buf := bufio.NewWriter(w)
		defer buf.Flush()

		server.RangeHostMetrics(func(host string, m *socks5.HostMetrics) {
			buf.WriteString(fmt.Sprintf("proxy_connect_tx{remote=\"%s\"} %d\n", host, m.Tx.Load()))
			buf.WriteString(fmt.Sprintf("proxy_connect_rx{remote=\"%s\"} %d\n", host, m.Rx.Load()))
			buf.WriteString(fmt.Sprintf("proxy_connect_active{remote=\"%s\"} %d\n", host, m.Active.Load()))
			for i := 0; i < len(m.Commands); i++ {
				buf.WriteString(fmt.Sprintf("proxy_connect_count{remote=\"%s\",command=\"%d\"} %d\n", host, i, m.Commands[i].Load()))
			}
		})
	})

	log.Printf("Starting status page on http://%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
