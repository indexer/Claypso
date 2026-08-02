package vault

import (
	"context"
	"fmt"
	"os"

	"github.com/yemon/calypso/internal/crypto"
)

// Rekey re-encrypts the vault under a new passphrase. The current on-disk
// blob (still encrypted with the old passphrase) is preserved at
// <path>.pre-rekey.bak first, so a mistyped-but-confirmed new passphrase is
// recoverable. Older auto-backups and exports remain encrypted with the old
// passphrase — the caller should surface that to the user.
func (v *Vault) Rekey(ctx context.Context, path string, newPassphrase []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := crypto.NewCipher(newPassphrase)
	if err != nil {
		return err
	}
	if cur, err := os.ReadFile(path); err == nil {
		if err := atomicWrite(path+".pre-rekey.bak", cur); err != nil {
			c.Clear()
			return fmt.Errorf("backup before rekey: %w", err)
		}
	}
	// Swap the cipher so writeUnlocked seals with the new key; restore on
	// failure so later saves in this process still use the working old key.
	old := v.cipher
	v.cipher = c
	if err := v.Save(ctx, path, newPassphrase); err != nil {
		v.cipher = old
		c.Clear()
		return err
	}
	if old != nil {
		old.Clear()
	}
	return nil
}
