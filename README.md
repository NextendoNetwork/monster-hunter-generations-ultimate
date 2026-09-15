<h1 align="center">monster-hunter-generations-ultimate</h1>

<p align="center">
  <b>Nextendo Network game server for Monster Hunter Generations Ultimate.</b>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/license-PolyForm%20Shield%201.0.0-orange" alt="License: PolyForm Shield 1.0.0">
  <img src="https://img.shields.io/badge/go-1.23%2B-00ADD8" alt="Go 1.23+">
</p>

---

## What is this?

The NEX game server for **Monster Hunter Generations Ultimate** on
[Nextendo Network](https://nextendo.network). It handles authentication and matchmaking, speaking
the same NEX protocol the retail servers did.

It is built on the [**nextendo-nex**](https://github.com/NextendoNetwork/nextendo-nex) core, which
provides the PRUDP transport, RMC layer, and common service protocols.

### A 3DS-lineage title, not a native Switch one

MHGU is a Nintendo Switch port of the 3DS's *Monster Hunter XX*. Its online code carries over the
original legacy 32-bit ARM codebase under an AArch64 shim — none of the NEX init logic is reachable
from the AArch64 side at all. That has two concrete, real consequences this server accounts for:

- **Access key and game server ID were never publicly documented** for this title (unlike most
  titles in this fleet, which have a [kinnay wiki](https://github.com/kinnay/NintendoClients/wiki)
  entry). Both were extracted directly from the game binary: the game server ID (`2896BD04`) came
  from a live capture of the real client's own DNS/TLS handshake attempt against Nintendo's
  still-live production server; the access key (`4152f312`) was then found by cross-referencing
  that confirmed constant in the game's ARM32 code — two independent `MOVW`+`MOVT` construction
  sites both resolve to the same isolated, null-terminated 8-character string. See the comment at
  the top of [`main.go`](main.go) for the full trail.
- **A much tighter CONNECT-packet size budget** than modern Switch NEX titles. The Kerberos ticket
  gets echoed back as part of the client's secure CONNECT handshake, and error `2306-0116`
  ("buffer too large to send") is a real, documented NEX Core error the client throws *before*
  transmitting anything — matching zero packets ever reaching the secure port. This server answers
  with Kerberos ticket version `0`, which drops the extra random-key wrapper (148 → 124 bytes), the
  only lever available to shrink it.
- **`securePID=2`** — the fixed "Quazal Rendez-Vous" account ID from the 3DS/Wii U PID convention
  (kinnay wiki's Kerberos-Authentication page, Special Accounts table), not the separate
  Switch-specific convention other titles in this fleet use.

## What's implemented

DataStore (`0x73`) and Utility (`0x6E`) are the two modules confirmed present from the game
binary's embedded SDK version strings (`SDK MW+Nintendo+NEX_DS-4_4_0`, `NEX_UT-4_4_0`), alongside
MatchMaking. No measured reference of MHGU's actual online init sequence exists yet, so every call
on those two modules is logged (to diff against real client traffic and implement iteratively) and
answered with a safe empty-list/not-found response so the game proceeds and reveals what it calls
next, instead of soft-locking on `NotImplemented` — see [`mhgu_init.go`](mhgu_init.go). Same
measured-then-implement starting point every other title in this fleet began from.

## Running

```sh
cp example.env .env    # then edit .env
go run .
```

Configuration is entirely through environment variables — see [`example.env`](example.env). No
secrets are baked into the source.

**Build note:** `go.mod` builds against a sibling
[`nextendo-nex`](https://github.com/NextendoNetwork/nextendo-nex) checkout
(`replace ... => ../nextendo-nex`). MHGU needs `AuthConfig.ContextResultTrailingU64` (its login
result carries a trailing u64) and `Matchmaking.OwnerLeaveUnregisters` (a hub closes for everyone
when its host leaves), so clone `nextendo-nex` at `main` alongside this repo.

## What this is not

This server ships **no** Nintendo code, keys, or copyrighted assets. It is an independent
reimplementation for use with a community-run replacement service, not affiliated with, endorsed by,
or associated with Nintendo. The NEX access key it uses is a well-known per-title value derivable
from the game itself, not a secret.

## License

Released under the **[PolyForm Shield License 1.0.0](LICENSE.md)** — source-available: read, use,
modify, and self-host, but do not use it to provide a product that competes with Nextendo Network.
