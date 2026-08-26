// Command mhgu runs the Monster Hunter Generations Ultimate online servers (auth +
// secure) on the Nextendo NEX stack.
//
// Access key and game server ID were NOT publicly documented for this title (unlike
// most other titles in this fleet, which have a kinnay wiki entry) -- both were
// extracted from the real game binary: the game server ID (0x2896bd04) came from a
// live capture of the real client's own DNS/WebSocket handshake attempt against
// Nintendo's still-live production server (citron's Service.SSL debug log, with
// Nextendo's redirect temporarily disabled so the real hostname resolution and TLS
// upgrade actually happened). The access key ("4152f312") was then found by
// cross-referencing that confirmed game-server-id constant in the game's ARM32 code
// (main links a legacy 32-bit ARM codebase -- almost certainly carried over from the
// 3DS original, Monster Hunter XX -- under an AArch64 shim; none of the NEX init code
// is reachable from the AArch64 side). Two independent MOVW+MOVT construction sites
// for the game-server-id both resolve their next struct field (PC-relative address
// arithmetic) to the same isolated, null-terminated 8-char string.
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"os"
	"strconv"
	"strings"
	"time"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

const (
	accessKey = "4152f312" // extracted from the game binary, see package comment above
	serverID  = "2896BD04" // same -- also the sni-router hostname suffix ("g2896bd04")

	nexVersion = 40400 // NEX 4.4.0, confirmed via embedded SDK version strings
	// securePID=2 is the real, fixed "Quazal Rendez-Vous" account id for the 3DS/Wii U
	// PID convention (kinnay's NintendoClients wiki, Kerberos-Authentication page's
	// Special Accounts table) -- MHGU is a 3DS-lineage title (raw PRUDP V1, legacy
	// socket conventions), so this table applies, not the separate Switch-specific one.
	securePID = 2

	sessionKeyLen = 32
)

var securePassword = envOr("NEXTENDO_SECURE_PASSWORD", "securepasswordplz1")

var (
	nextendoHost = envOr("NEXTENDO_HOST", "127.0.0.1")
	authPort     = envOrInt("AUTH_PORT", 443)
	securePort   = envOrInt("SECURE_PORT", 60010)
	certFile     = envOr("CERT_FILE", "cert.pem")
	keyFile      = envOr("KEY_FILE", "key.pem")

	nextendoSecret = loadNextendoSecret()
	requireAccount = os.Getenv("NEXTENDO_REQUIRE_ACCOUNT") == "1"
)

func main() {
	settings := nex.NewSwitchSettings(accessKey, nexVersion)
	// MHGU's legacy 3DS-derived PRUDP stack has a much tighter CONNECT-packet size
	// budget than modern Switch NEX titles -- the ticket gets echoed back as part of
	// the client's secure CONNECT handshake, and error 2306-0116 ("buffer too large
	// to send") is a real, documented NEX Core error the client throws BEFORE ever
	// transmitting, which matches zero packets ever reaching the secure port. Ticket
	// version 0 drops the extra random-key wrapper (148 -> 124 bytes), the only lever
	// we have to shrink it.
	settings.KerberosTicketVersion = 0

	// --- Auth server (:443 via sni-router) ---
	secureURL := nex.NewStationURL("prudps")
	secureURL.Set("address", nextendoHost)
	secureURL.SetInt("port", securePort)
	secureURL.SetInt("CID", 1)
	secureURL.SetInt("PID", securePID)
	secureURL.SetInt("sid", 1)
	secureURL.SetInt("stream", 10)
	secureURL.SetInt("type", 3) // BehindNAT|Public -- MHGU's Pia is 3DS-derived, older than Splatoon 2's (which needs this same 0x03, not the newer 0x0B Switch titles use)

	authEndpoint := nex.NewEndpoint(settings)
	authCfg := &nex.AuthConfig{
		Settings:         settings,
		SecurePID:        securePID,
		SecurePassword:   securePassword,
		SecureStationURL: secureURL,
		ServerName:       "Monster Hunter Generations Ultimate",
		SessionKeyLength: sessionKeyLen,
		ResolveUser:      resolveUser,
		// MHGU is a legacy, 3DS-derived title (unlike SSBU, which method 0x6's
		// flat response shape was tuned for) -- try the older, fuller
		// RVConnectionData shape LoginEx uses instead, since the flat shape has
		// produced a persistent, invariant Core::BufferOverflow (2306-0116).
		UseFullConnectionDataForContext: true,
	}
	authEndpoint.Register(nex.ProtocolTicketGranting, authCfg.Handler())
	authEndpoint.OnRMC = logRMC("Auth")
	authServer := nex.NewServer(authEndpoint)

	// --- Secure server ---
	secureSettings := *settings
	secureSettings.PrudpMinorVersion = 0
	secureEndpoint := nex.NewEndpoint(&secureSettings)
	secureEndpoint.SetSecureAccount(securePassword, securePID)

	mm := nex.NewMatchmaking()
	secureEndpoint.Register(nex.ProtocolSecureConnection, nex.SecureConnectionHandler())
	secureEndpoint.Register(nex.ProtocolMatchmakeExtension, mm.ExtensionHandler())
	secureEndpoint.Register(nex.ProtocolMatchMaking, mm.MatchMakingHandler())
	secureEndpoint.Register(nex.ProtocolMatchMakingExt, mm.MatchMakingExtHandler())
	secureEndpoint.Register(nex.ProtocolNATTraversal, nex.NATTraversalHandler())
	// DataStore (0x73) + Utility (0x6E): confirmed modules (embedded SDK version
	// strings "SDK MW+Nintendo+NEX_DS-4_4_0" / "NEX_UT-4_4_0"), no measured client
	// traffic yet. Logging + safe-empty-default handler, same starting point every
	// other title in this fleet began from -- real per-method struct/list shapes get
	// filled in once a real client's actual calls are observed.
	setupMHGUInit(secureEndpoint)

	logSecure := logRMC("Secure")
	secureEndpoint.OnRMC = func(c *nex.Connection, req *nex.RMCMessage) {
		logSecure(c, req)
		noteRMC(c, req)
	}
	secureEndpoint.OnNATProperties = noteNAT
	secureEndpoint.OnConnect = func(c *nex.Connection) {
		fmt.Printf("[MHGU Secure] connected pid=%d id=%d addr=%s\n", c.PID, c.ID, c.RemoteAddr)
	}
	secureEndpoint.OnDisconnect = func(c *nex.Connection) {
		mm.RemovePlayer(c.PID)
	}
	secureServer := nex.NewServer(secureEndpoint)

	secureEndpoint.StartReaper()
	go startDashboard(secureEndpoint, mm)

	proxyProto := os.Getenv("NEXTENDO_PROXY_PROTOCOL") == "1"
	go func() {
		fmt.Printf("[MHGU Auth] listening WSS :%d (proxyProto=%v, secure URL -> %s)\n", authPort, proxyProto, secureURL.String())
		var err error
		if proxyProto {
			err = authServer.ListenSecureProxy(authPort, certFile, keyFile)
		} else {
			err = authServer.ListenSecure(authPort, certFile, keyFile)
		}
		if err != nil {
			fmt.Printf("[MHGU Auth] stopped: %v\n", err)
		}
	}()

	// Raw PRUDP-V1-over-UDP secure listener, alongside the existing WSS one --
	// see prudp_udp.go's package comment for why: the ARM32 decompile shows
	// MHGU's actual connectionType-9 secure-connect code opens a real UDP
	// socket, never a WebSocket, and Pretendo's proven MH4U server (same
	// game engine lineage) listens with plain raw UDP for both auth and
	// secure. Bound to the same port number as the WSS listener -- TCP and
	// UDP are independent namespaces, so this doesn't conflict.
	go func() {
		udpServer := &nex.UDPServer{
			Settings:  &secureSettings,
			SecureKey: secureEndpoint.SecureKey,
		}
		if err := udpServer.ListenUDP(securePort); err != nil {
			fmt.Printf("[MHGU Secure UDP] stopped: %v\n", err)
		}
	}()

	fmt.Printf("[MHGU Secure] listening WSS :%d\n", securePort)
	if err := secureServer.ListenSecure(securePort, certFile, keyFile); err != nil {
		fmt.Printf("[MHGU Secure] stopped: %v\n", err)
	}
}

// resolveUser: identical shape to every other Nextendo game server (see
// mario-tennis-aces/main.go, the reference implementation for signed-token
// enforcement).
func resolveUser(username string, extraData []byte) (uint64, []byte, bool) {
	fmt.Printf("[Auth][diag] raw extraData (%d bytes): %x\n", len(extraData), extraData)
	sk := sha256.Sum256([]byte("nextendo-src:" + username))
	sourceKey := sk[:]

	if pid, ok := nextendoPIDFromToken(username); ok {
		if allow, reason := nextendoOnlineCheck(pid, "ryujinx"); !allow {
			fmt.Printf("[Auth] pid=%d online REFUSED (%s)\n", pid, reason)
			return 0, nil, false
		}
		return pid, sourceKey, true
	}

	if n, err := strconv.ParseUint(username, 10, 64); err == nil && n >= 1800000000 {
		provenPID, proven := uint64(0), false
		if tok, ok := nex.NexTokenFromLoginExtraData(extraData); ok {
			provenPID, proven = nextendoPIDFromToken(tok)
		}
		if n < 1810000000 {
			switch {
			case proven && provenPID == n:
				fmt.Printf("[Auth][bind] pid=%d OK: nx2 proves the PID\n", n)
			case proven && provenPID != n:
				fmt.Printf("[Auth][bind] pid=%d IMPERSONATION: nx2 proves %d, not %d\n", n, provenPID, n)
			default:
				fmt.Printf("[Auth][bind] pid=%d NO PROOF: no nx2 in extraData (build < 1.7.1?)\n", n)
			}
			if requireSignedToken() && !(proven && provenPID == n) {
				fmt.Printf("[Auth] pid=%d REFUSED: identity not proven (signed nx2 token required)\n", n)
				return 0, nil, false
			}
		}
		pid, kind := n, "ryujinx"
		if n >= 1810000000 {
			kind = "switch"
			rp, st := resolveNSAtoPID(n)
			switch st {
			case nsaOK:
				pid = rp
			case nsaUnknown:
				fmt.Printf("[Auth] NSA %d REFUSED (no Nextendo account)\n", n)
				return 0, nil, false
			case nsaUnreachable:
				fmt.Printf("[Auth] NSA %d REFUSED (account server unreachable)\n", n)
				return 0, nil, false
			}
		}
		if allow, reason := nextendoOnlineCheck(pid, kind); !allow {
			fmt.Printf("[Auth] pid=%d online REFUSED (%s)\n", pid, reason)
			return 0, nil, false
		}
		return pid, sourceKey, true
	}

	if requireAccount {
		fmt.Printf("[Auth] anonymous login REFUSED (Nextendo account required): %q\n", username)
		return 0, nil, false
	}
	return anonymousPID(username), sourceKey, true
}

func nextendoPIDFromToken(s string) (uint64, bool) {
	if len(nextendoSecret) == 0 || !strings.HasPrefix(s, "nx2.") {
		return 0, false
	}
	parts := strings.Split(s[len("nx2."):], ".")
	if len(parts) != 2 {
		return 0, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, false
	}
	mac := hmac.New(sha256.New, nextendoSecret)
	mac.Write([]byte("nex:" + string(raw)))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[1])) {
		return 0, false
	}
	f := strings.SplitN(string(raw), ".", 3)
	if len(f) != 3 {
		return 0, false
	}
	pid, err := strconv.ParseUint(f[0], 10, 64)
	if err != nil {
		return 0, false
	}
	if exp, err := strconv.ParseInt(f[2], 10, 64); err != nil || time.Now().Unix() > exp {
		return 0, false
	}
	return pid, true
}

func loadNextendoSecret() []byte {
	if v := os.Getenv("NEXTENDO_SECRET"); v != "" {
		return []byte(v)
	}
	path := envOr("NEXTENDO_SECRET_FILE", "nextendo_secret.key")
	if b, err := os.ReadFile(path); err == nil {
		if dec, derr := hex.DecodeString(strings.TrimSpace(string(b))); derr == nil && len(dec) >= 16 {
			return dec
		}
	}
	return nil
}

func anonymousPID(username string) uint64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(username))
	return 1800000000 + uint64(h.Sum32()%100000000)
}

func logRMC(tag string) func(*nex.Connection, *nex.RMCMessage) {
	return func(c *nex.Connection, req *nex.RMCMessage) {
		fmt.Printf("[MHGU %s] pid=%d proto=%#x method=%d call=%d\n", tag, c.PID, req.Protocol, req.Method, req.CallID)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func requireSignedToken() bool {
	v := os.Getenv("NEXTENDO_REQUIRE_SIGNED_TOKEN")
	return v == "1" || v == "true"
}
