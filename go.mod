module github.com/luxfi/bench

go 1.26.4

// Local replace directives so the bench harness builds against the
// in-tree luxfi modules. Drop these once we tag and pin a release.

require (
	github.com/luxfi/codec v1.2.1
	github.com/luxfi/zap v1.2.6
)

require (
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/cloudflare/circl v1.6.3 // indirect
	github.com/grandcat/zeroconf v1.0.0 // indirect
	github.com/luxfi/accel v1.2.4 // indirect
	github.com/luxfi/container v0.2.1 // indirect
	github.com/luxfi/crypto v1.20.2 // indirect
	github.com/luxfi/ids v1.3.2 // indirect
	github.com/luxfi/math v1.5.1 // indirect
	github.com/luxfi/math/big v0.1.0 // indirect
	github.com/luxfi/mdns v0.1.1 // indirect
	github.com/luxfi/mock v0.1.1 // indirect
	github.com/luxfi/sampler v1.1.0 // indirect
	github.com/miekg/dns v1.1.72 // indirect
	github.com/mr-tron/base58 v1.3.0 // indirect
	go.uber.org/mock v0.6.0 // indirect
	golang.org/x/crypto v0.52.0 // indirect
	golang.org/x/exp v0.0.0-20260529124908-c761662dc8c9 // indirect
	golang.org/x/mod v0.36.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/tools v0.45.0 // indirect
	gonum.org/v1/gonum v0.17.0 // indirect
)
