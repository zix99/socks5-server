package main

import (
	"bufio"
	"fmt"
	"net/http"
	"runtime"
	"socks5-server-ng/pkg/bufpool"
	"socks5-server-ng/pkg/go-socks5"
	"sort"
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
				<th>UDP</th>
				<th>Rx</th>
				<th>Tx</th>
			</tr>
			{{range $host := .Hosts}}
			<tr>
				<td>{{$host.Host}}</td>
				<td>{{$host.LastSeen.Format "2006-01-02 15:04:05"}}</td>
				<td>{{$host.Active}}</td>
				<td>{{$host.ActiveUDP}}</td>
				<td>{{$host.Rx}}</td>
				<td>{{$host.Tx}}</td>
			</tr>
			{{end}}
		</table>
		<h2>Targets</h2>
		<table border="1" cellspacing="0" cellpadding="4">
			<tr>
				<th>Host</th>
				<th>Active</th>
				<th>Rx</th>
				<th>Tx</th>
			</tr>
			{{range $host := .Targets}}
			<tr>
				<td>{{$host.Host}}</td>
				<td>{{$host.Active}}</td>
				<td>{{$host.Rx}}</td>
				<td>{{$host.Tx}}</td>
			</tr>
			{{end}}
		</table>
		<h2>Global</h2>
		<strong>Hosts:</strong> {{.HostCount}}<br>
		<strong>Rx:</strong> {{.Rx}} <strong>Tx:</strong> {{.Tx}}<br>
		<h2>Runtime</h2>
		{{.RuntimeMetrics}}<br />
		Pool: {{.PoolMetrics}}
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
	Host      string
	LastSeen  time.Time
	Active    int64
	ActiveUDP int64
	Rx, Tx    ByteSize
}

type StatusModel struct {
	Hosts          []StatusModelHost
	Targets        []StatusModelHost
	RuntimeMetrics string
	PoolMetrics    string
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
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		pool := bufpool.Pool4096
		model := &StatusModel{
			RuntimeMetrics: fmt.Sprintf("Heap=%d, InUse=%d, Total=%d, Sys=%d, NumGC=%d, GoRoutines=%d", stats.HeapAlloc, stats.HeapInuse, stats.TotalAlloc, stats.Sys, stats.NumGC, runtime.NumGoroutine()),
			PoolMetrics:    fmt.Sprintf("Size=%d/%d, Leased=%d, Misses=%d", pool.MetricPoolSize(), pool.MetricMaxSize(), pool.MetricLeased(), pool.MetricMisses()),
		}
		server.RangeHostMetrics(func(host string, m *socks5.HostMetrics) {
			model.Hosts = append(model.Hosts, StatusModelHost{
				Host:      host,
				LastSeen:  m.LastSeen.Load().(time.Time),
				Active:    m.Active.Load(),
				ActiveUDP: m.ActiveUDP.Load(),
				Rx:        ByteSize(m.Rx.Load()),
				Tx:        ByteSize(m.Tx.Load()),
			})
		})
		sort.Slice(model.Hosts, func(i, j int) bool {
			return model.Hosts[i].Rx > model.Hosts[j].Rx
		})

		server.RangeTargetMetrics(func(target string, m *socks5.NetMetrics) {
			model.Targets = append(model.Targets, StatusModelHost{
				Host:   target,
				Active: m.Active.Load(),
				Tx:     ByteSize(m.Tx.Load()),
				Rx:     ByteSize(m.Rx.Load()),
			})
		})
		sort.Slice(model.Targets, func(i, j int) bool {
			if model.Targets[i].Active == model.Targets[j].Active {
				return model.Targets[i].Rx > model.Targets[j].Rx
			}
			return model.Targets[i].Active > model.Targets[j].Active
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
			buf.WriteString(fmt.Sprintf("proxy_connect_active_udp{remote=\"%s\"} %d\n", host, m.ActiveUDP.Load()))
			for i := 0; i < len(m.Commands); i++ {
				buf.WriteString(fmt.Sprintf("proxy_connect_count{remote=\"%s\",command=\"%d\"} %d\n", host, i, m.Commands[i].Load()))
			}
		})

		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)

		p := make([]runtime.StackRecord, 32)
		numThreads, _ := runtime.ThreadCreateProfile(p)

		buf.WriteString("# Go metrics\n")
		buf.WriteString(fmt.Sprintf("go_goroutines %d\n", runtime.NumGoroutine()))
		buf.WriteString(fmt.Sprintf("go_threads %d\n", numThreads))
		buf.WriteString(fmt.Sprintf("go_info %s\n", runtime.Version()))

		buf.WriteString(fmt.Sprintf("go_memstats_alloc_bytes %d\n", stats.HeapAlloc))
		buf.WriteString(fmt.Sprintf("go_memstats_heap_alloc_bytes %d\n", stats.HeapAlloc))
		buf.WriteString(fmt.Sprintf("go_memstats_heap_sys_bytes %d\n", stats.HeapSys))
		buf.WriteString(fmt.Sprintf("go_memstats_heap_idle_bytes %d\n", stats.HeapIdle))
		buf.WriteString(fmt.Sprintf("go_memstats_heap_inuse_bytes %d\n", stats.HeapInuse))
		buf.WriteString(fmt.Sprintf("go_memstats_heap_released_bytes %d\n", stats.HeapReleased))
		buf.WriteString(fmt.Sprintf("go_memstats_heap_objects %d\n", stats.HeapObjects))

		buf.WriteString(fmt.Sprintf("go_memstats_stack_inuse_bytes %d\n", stats.StackInuse))
		buf.WriteString(fmt.Sprintf("go_memstats_stack_sys_bytes %d\n", stats.StackSys))

	})

	logrus.Printf("Starting status page on http://%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		logrus.Fatal(err)
	}
}
