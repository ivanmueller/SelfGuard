module selfguard

go 1.21

require (
	github.com/jchv/go-webview2 v0.0.0-20260205173254-56598839c808
	github.com/kardianos/service v1.2.2
	github.com/miekg/dns v1.1.58
	golang.org/x/sys v0.17.0
)

require (
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
	golang.org/x/mod v0.14.0 // indirect
	golang.org/x/net v0.20.0 // indirect
	golang.org/x/tools v0.17.0 // indirect
)

replace (
	golang.org/x/crypto => github.com/golang/crypto v0.18.0
	golang.org/x/exp => github.com/golang/exp v0.0.0-20240119083558-1b970713d09a
	golang.org/x/mod => github.com/golang/mod v0.14.0
	golang.org/x/net => github.com/golang/net v0.20.0
	golang.org/x/sync => github.com/golang/sync v0.6.0
	golang.org/x/sys => github.com/golang/sys v0.17.0
	golang.org/x/term => github.com/golang/term v0.16.0
	golang.org/x/text => github.com/golang/text v0.14.0
	golang.org/x/tools => github.com/golang/tools v0.17.0
)
