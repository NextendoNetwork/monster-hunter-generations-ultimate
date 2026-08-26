package main

// MHGU online-init handler for DataStore (0x73) + Utility (0x6E) -- the two
// confirmed modules (besides MatchMaking) from the game binary's embedded SDK
// version strings ("SDK MW+Nintendo+NEX_DS-4_4_0", "NEX_UT-4_4_0"). No measured
// reference of MHGU's actual online init exists yet, so every call is LOGGED (to
// diff against real client traffic and implement iteratively) and answered with a
// safe empty-list (count=0) so the game PROCEEDS and reveals what it calls next,
// instead of soft-locking on NotImplemented. Same measured-then-implement starting
// point every other title in this fleet began from -- no MHGU-specific struct/list
// shapes are guessed here.

import (
	"fmt"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

func setupMHGUInit(endpoint *nex.Endpoint) {
	endpoint.Register(0x73, mhguInitHandler(0x73))
	endpoint.Register(0x6E, mhguInitHandler(0x6E))
	fmt.Printf("[MHGU Init] handlers registered: DataStore(0x73) + Utility(0x6E)\n")
}

func mhguInitHandler(proto uint16) nex.RMCHandler {
	return func(conn *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		s := conn.Settings

		// 0x73.8 = DataStore::GetMeta-style lookup; Nintendo answers NotFound and
		// the client continues rather than treating it as fatal.
		if proto == 0x73 && req.Method == 8 {
			fmt.Printf("[MHGU Init] 0x73.8 -> NotFound 0x80690004\n")
			return nex.NewRMCError(s, proto, req.CallID, 0x80690004)
		}

		out := nex.NewStreamOut(s)
		out.U32(0)
		fmt.Printf("[MHGU Init] 0x%02x.%d call=%d bodyLen=%d hex=%x -> empty-list\n",
			proto, req.Method, req.CallID, len(req.Body), req.Body)
		return nex.NewRMCSuccess(s, proto, req.Method, req.CallID, out.Bytes())
	}
}
