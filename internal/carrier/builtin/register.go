// Package builtin registers the built-in carrier implementations.
package builtin

import (
	"context"

	"github.com/svllvsxprod/librertc-core/internal/carrier"
	"github.com/svllvsxprod/librertc-core/internal/provider"
	"github.com/svllvsxprod/librertc-core/internal/provider/jazz"
	"github.com/svllvsxprod/librertc-core/internal/provider/telemost"
	"github.com/svllvsxprod/librertc-core/internal/provider/wbstream"
)

type providerFactory func(context.Context, provider.Config) (provider.Provider, error)

// Register wires the built-in carriers into the carrier registry.
func Register() {
	registerProvider("jazz", jazz.New)
	registerProvider("telemost", telemost.New)
	registerProvider("wbstream", wbstream.New)
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
