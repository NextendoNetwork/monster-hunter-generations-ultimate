module github.com/NextendoNetwork/monster-hunter-generations-ultimate

go 1.23.0

require github.com/NextendoNetwork/nextendo-nex v0.1.4

require (
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/lxzan/gws v1.10.0 // indirect
)

// MHGU's legacy PRUDP V1/UDP transport (see main.go) needs nextendo-nex's UDPServer type and
// AuthConfig.UseFullConnectionDataForContext, neither of which exist in the published v0.1.4
// above yet -- point at a local checkout with that work until a new release includes it.
replace github.com/NextendoNetwork/nextendo-nex => ../nextendo-nex
