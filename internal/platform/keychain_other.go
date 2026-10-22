//go:build !darwin

package platform

import "context"

// KeychainPassword reports no item: only macOS has a Keychain.
func KeychainPassword(ctx context.Context, service string) (secret string, found bool, err error) {
	return "", false, nil
}
