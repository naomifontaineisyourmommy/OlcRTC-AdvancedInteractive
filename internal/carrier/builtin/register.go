// Package builtin registers the built-in carrier implementations.
package builtin

import (
	"context"

	authWBStream "github.com/openlibrecommunity/olcrtc/internal/auth/wbstream"
	"github.com/openlibrecommunity/olcrtc/internal/carrier"
	_ "github.com/openlibrecommunity/olcrtc/internal/engine/livekit" // engine registration via init
	"github.com/openlibrecommunity/olcrtc/internal/provider"
	"github.com/openlibrecommunity/olcrtc/internal/provider/jazz"
	"github.com/openlibrecommunity/olcrtc/internal/provider/telemost"
)

type providerFactory func(context.Context, provider.Config) (provider.Provider, error)

// Register wires the built-in carriers into the carrier registry.
func Register() {
	// Legacy provider-based carriers (still being migrated to engine+auth).
	registerProvider("jazz", jazz.New)
	registerProvider("telemost", telemost.New)

	// Migrated to engine+auth: WB Stream now goes through the LiveKit engine
	// with the wbstream auth provider.
	registerEngineAuth("wbstream", authWBStream.Provider{})
}

func registerProvider(name string, factory providerFactory) {
	carrier.Register(name, func(ctx context.Context, cfg carrier.Config) (carrier.Session, error) {
		prov, err := factory(ctx, provider.Config{
			RoomURL:   cfg.RoomURL,
			Name:      cfg.Name,
			OnData:    cfg.OnData,
			DNSServer: cfg.DNSServer,
			ProxyAddr: cfg.ProxyAddr,
			ProxyPort: cfg.ProxyPort,
		})
		if err != nil {
			return nil, err
		}
		return &providerSession{provider: prov}, nil
	})
}
