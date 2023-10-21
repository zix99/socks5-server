package main

import (
	"bufio"
	"fmt"
	"net/http"
	"socks5-server-ng/pkg/go-socks5"
	"text/template"
	"time"

	"github.com/sirupsen/logrus"
)

var statusTemplate = template.Must(template.New("status").Parse(`
<html>
	<body>
		<h1>Socks5 Proxy</h1>
		<h2>Active Hosts</h2>
		<table border="1" cellspacing="0" cellpadding="4">
			<tr>
				<th>Host</th>
				<th>Last Seen</th>
				<th>Active</th>
				<th>Rx</th>
				<th>Tx</th>
			</tr>
			{{range $host := .Hosts}}
			<tr>
				<td>{{$host.Host}}</td>
				<td>{{$host.LastSeen.Format "2006-01-02 15:04:05"}}</td>
				<td>{{$host.Active}}</td>
				<td>{{$host.Rx}}</td>
				<td>{{$host.Tx}}</td>
			</tr>
			{{end}}
		</table>
		<h2>Global</h2>
		<strong>Hosts:</strong> {{.HostCount}}<br>
		<strong>Rx:</strong> {{.Rx}} <strong>Tx:</strong> {{.Tx}}<br>
		<hr />
		<a href="/metrics">Prometheus Metrics</a>
	</body>
</html>
`))

type ByteSize int64

var byteUnits = []string{"B", "KB", "MB", "GB", "TB", "PB"}

func (s ByteSize) String() string {
	unit := 0
	sf := float64(s)
	for sf > 1024 && unit < len(byteUnits)-1 {
		sf /= 1024
		unit++
	}
	return fmt.Sprintf("%.1f %s", sf, byteUnits[unit])
}

type StatusModelHost struct {
	Host     string
	LastSeen time.Time
	Active   int64
	Rx, Tx   ByteSize
}

type StatusModel struct {
	Hosts []StatusModelHost
}

func (s *StatusModel) Rx() (ret ByteSize) {
	for _, host := range s.Hosts {
		ret += host.Rx
	}
	return
}

func (s *StatusModel) Tx() (ret ByteSize) {
	for _, host := range s.Hosts {
		ret += host.Tx
	}
	return
}

func (s *StatusModel) HostCount() int {
	return len(s.Hosts)
}

func serveStatusPage(server *socks5.Server, addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		model := &StatusModel{}
		server.RangeHostMetrics(func(host string, m *socks5.HostMetrics) {
			model.Hosts = append(model.Hosts, StatusModelHost{
				Host:     host,
				LastSeen: m.LastSeen.Load().(time.Time),
				Active:   m.Active.Load(),
				Rx:       ByteSize(m.Rx.Load()),
				Tx:       ByteSize(m.Tx.Load()),
			})
		})
		statusTemplate.Execute(w, &model)
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

	logrus.Printf("Starting status page on http://%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		logrus.Fatal(err)
	}
}
