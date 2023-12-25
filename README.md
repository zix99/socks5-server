# go-socks5-proxy-ng

Based on [serjs/socks5-server](https://github.com/serjs/socks5-server) with modern go features, UDP Associate support, metrics, and more configurability.

Simple socks5 server using go-socks5 with authentication, allowed ips list and destination FQDNs filtering

# Examples

- Run docker container using default container port 1080 and expose it to world using host port 1080, with auth creds

    ```docker run -d --name socks5 -p 1080:1080 -e PROXY_USER=<PROXY_USER> -e PROXY_PASSWORD=<PROXY_PASSWORD>  serjs/go-socks5-proxy```

    - Leave `PROXY_USER` and `PROXY_PASSWORD` empty for skip authentication options while running socks5 server, see example below

- Run docker container using specifit container port and expose it to host port 1090, without auth creds

    ```docker run -d --name socks5 -p 1090:9090 -e PROXY_PORT=9090 serjs/go-socks5-proxy```

# List of supported config parameters

|ENV variable|Type|Default|Description|
|------------|----|-------|-----------|
|PROXY_USER|String|EMPTY|Set proxy user (also required existed PROXY_PASS)|
|PROXY_PASSWORD|String|EMPTY|Set proxy password for auth, used with PROXY_USER|
|PROXY_PORT|String|1080|Set listen port for application inside docker container|
|PROXY_STATUS_PORT|String|unset|Set port for http status page|
|PROXY_RESOLVER|String|unset|Set DNS server, defaults to system|
|PROXY_RESOLVER_NET|String|ip4|How to resolve domains|
|PROXY_REQUIRE_FQDN|Bool|false|If set, requires fully qualified domain to connect|
|PROXY_VERBOSE|bool|false|If set, more verbose logging|
|ALLOWED_DEST_FQDN|String|EMPTY|Allowed destination address regular expression pattern. Default allows all.|
|ALLOWED_CIDR|[]String|Empty|Set allowed CIDR spaces that can connect to proxy, separator `,`|


# Build your own image:
`docker-compose -f docker-compose.build.yml up -d`\
Just don't forget to set parameters in the `.env` file.

# Test running service

Assuming that you are using container on 1080 host docker port

## Without authentication

```curl --socks5 <docker host ip>:1080  https://ifcfg.co``` - result must show docker host ip (for bridged network)

or

```docker run --rm curlimages/curl:7.65.3 -s --socks5 <docker host ip>:1080 https://ifcfg.co```

## With authentication

```curl --socks5 <docker host ip>:1080 -U <PROXY_USER>:<PROXY_PASSWORD> http://ifcfg.co```

or

```docker run --rm curlimages/curl:7.65.3 -s --socks5 <PROXY_USER>:<PROXY_PASSWORD>@<docker host ip>:1080 http://ifcfg.co```

# Authors

* **Sergey Bogayrets**

See also the list of [contributors](https://github.com/zix99/socks5-server/graphs/contributors) who participated in this project.

# License
MIT
