package analysis

import (
	"cmp"
	"errors"
	"io/fs"
	"slices"

	"github.com/yemon/calypso/internal/project"
)

// DriftReport summarises how one env's vault contents differ from the
// .env file currently on disk at the env's path.
//
// Counts mirror DiffCounts so the CLI can render the familiar
// "+a ~b -c =d" line. Entries (when populated by DriftDetails) carry the
// per-key story for users who pass --details.
type DriftReport struct {
	Ref     EnvRef
	Path    string
	Missing bool // true if the .env file doesn't exist on disk
	Counts  VarCounts
	Entries []DriftEntry // populated only by DriftDetails
}

// DriftKind classifies one key's relationship between vault and disk.
type DriftKind int

const (
	DriftSame DriftKind = iota
	DriftChanged
	DriftOnlyInVault // vault has it, disk doesn't
	DriftOnlyOnDisk  // disk has it, vault doesn't
)

func (k DriftKind) String() string {
	switch k {
	case DriftSame:
		return "same"
	case DriftChanged:
		return "changed"
	case DriftOnlyInVault:
		return "only in vault"
	case DriftOnlyOnDisk:
		return "only on disk"
	}
	return "?"
}

// DriftEntry is one row of a per-key drift listing.
type DriftEntry struct {
	Key      string
	VaultVal string
	DiskVal  string
	Kind     DriftKind
}

// HasDrift returns true when the report shows any divergence between vault
// and disk. Used by the CLI to set a non-zero exit code for CI.
func (r DriftReport) HasDrift() bool {
	if r.Missing {
		return true
	}
	c := r.Counts
	return c.Added > 0 || c.Removed > 0 || c.Changed > 0
}

// readDiskVars is the indirection point that lets tests substitute a fake
// disk reader. The real implementation calls project.ReadEnvFile.
var readDiskVars = func(path string) ([]project.Var, error) {
	return project.ReadEnvFile(path)
}

// Drift compares one resolved env against its on-disk .env file and
// returns counts only (cheap path used by the default `calypso drift`
// command). Use DriftDetails to additionally collect per-key entries.
func Drift(e *project.Environment, projectName string) (DriftReport, error) {
	return driftCore(e, projectName, false)
}

// DriftDetails is Drift plus the per-key entry list, for --details output.
func DriftDetails(e *project.Environment, projectName string) (DriftReport, error) {
	return driftCore(e, projectName, true)
}

func driftCore(e *project.Environment, projectName string, withEntries bool) (DriftReport, error) {
	r := DriftReport{Ref: EnvRef{Project: projectName, Env: e.Name}, Path: e.Path}
	disk, err := readDiskVars(e.Path)
	if err != nil {
		// If the file is missing, treat every vault key as "only in vault".
		// Other errors (permissions, etc.) are real and surfaced.
		if isNotExist(err) {
			r.Missing = true
			r.Counts.Removed = len(e.Vars) // "removed from disk relative to vault"
			if withEntries {
				for _, kv := range e.Vars {
					r.Entries = append(r.Entries, DriftEntry{
						Key: kv.Key, VaultVal: kv.Value.Reveal(), Kind: DriftOnlyInVault,
					})
				}
				sortDriftEntries(r.Entries)
			}
			return r, nil
		}
		return r, err
	}
	r.Counts = DiffCounts(e.Vars, disk)

	if !withEntries {
		return r, nil
	}

	vaultMap := make(map[string]string, len(e.Vars))
	for _, v := range e.Vars {
		vaultMap[v.Key] = v.Value.Reveal()
	}
	diskMap := make(map[string]string, len(disk))
	for _, v := range disk {
		diskMap[v.Key] = v.Value.Reveal()
	}
	seen := make(map[string]bool, len(vaultMap)+len(diskMap))
	for k, vv := range vaultMap {
		seen[k] = true
		dv, ok := diskMap[k]
		switch {
		case !ok:
			r.Entries = append(r.Entries, DriftEntry{Key: k, VaultVal: vv, Kind: DriftOnlyInVault})
		case vv != dv:
			r.Entries = append(r.Entries, DriftEntry{Key: k, VaultVal: vv, DiskVal: dv, Kind: DriftChanged})
		default:
			r.Entries = append(r.Entries, DriftEntry{Key: k, VaultVal: vv, DiskVal: dv, Kind: DriftSame})
		}
	}
	for k, dv := range diskMap {
		if seen[k] {
			continue
		}
		r.Entries = append(r.Entries, DriftEntry{Key: k, DiskVal: dv, Kind: DriftOnlyOnDisk})
	}
	sortDriftEntries(r.Entries)
	return r, nil
}

func sortDriftEntries(es []DriftEntry) {
	slices.SortFunc(es, func(a, b DriftEntry) int { return cmp.Compare(a.Key, b.Key) })
}

// isNotExist matches the standard library's "file does not exist" error
// regardless of which reader produced it (os.ReadFile via project.ReadEnvFile,
// or a test fake using fs.ErrNotExist).
func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
