module github.com/NextendoNetwork/monster-hunter-generations-ultimate

go 1.23.0

require github.com/NextendoNetwork/nextendo-nex v0.1.4

require (
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/lxzan/gws v1.10.0 // indirect
)

// Needs nextendo-nex main (AuthConfig.ContextResultTrailingU64, Matchmaking.OwnerLeaveUnregisters).
replace github.com/NextendoNetwork/nextendo-nex => ../nextendo-nex
