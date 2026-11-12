package phone

import (
	"context"

	oeerrors "github.com/oekit/pkg/errors"

	"github.com/yi-nology/open-voice-bridge/internal/remap"
)

// KeyTransport forwards one logical key press/release to the root remap
// helper. helper.Client satisfies this via its Inject method.
type KeyTransport interface {
	Inject(ctx context.Context, key string, value int) error
}

// KeyInjector enforces the logical-key whitelist locally before touching the
// helper socket, so an unpaired or malformed request never reaches the root
// process. The helper independently re-checks the same whitelist and the same
// power-key policy.
type KeyInjector struct {
	transport  KeyTransport
	allowPower bool
}

func NewKeyInjector(transport KeyTransport, allowPower bool) *KeyInjector {
	return &KeyInjector{transport: transport, allowPower: allowPower}
}

// Allowed reports whether a logical key may be injected at all.
func (k *KeyInjector) Allowed(key string) bool { return remap.InjectAllowed(key, k.allowPower) }

var (
	errKeyNotAllowed = oeerrors.New("key not allowed")
	errInvalidValue  = oeerrors.New("invalid key value")
	errNoHelper      = oeerrors.New("remap helper unavailable")
)

// Inject validates against the whitelist and value domain, then forwards to the
// helper transport.
func (k *KeyInjector) Inject(ctx context.Context, key string, value int) error {
	if !remap.InjectAllowed(key, k.allowPower) {
		return errKeyNotAllowed
	}
	if value != 0 && value != 1 {
		return errInvalidValue
	}
	if k.transport == nil {
		return errNoHelper
	}
	return k.transport.Inject(ctx, key, value)
}
