package analysis

import (
	"os"
	"time"

	"github.com/yemon/calypso/internal/project"
)

// MinExposedLen mirrors the CLI scrubber's threshold: values shorter than
// this are not treated as secrets, so they never mark a file as exposed.
const MinExposedLen = 4

// ExposureReport describes whether an env's on-disk .env currently holds
// real vault values, and for how long.
type ExposureReport struct {
	Ref     string
	Path    string
	Missing bool          // no .env on disk — nothing exposed
	HotKeys []string      // keys whose on-disk value equals the vault's real value
	Age     time.Duration // time since the file was last written
}

// Hot reports whether at least one real value is sitting on disk.
func (r ExposureReport) Hot() bool { return len(r.HotKeys) > 0 }

// Exposure reports which of e's real values are sitting in plaintext in its
// on-disk .env. A key is hot when the disk value matches the vault value
// exactly and is at least MinExposedLen bytes — placeholder (****), empty,
// and rotated-away values never count, so DEBUG=1 style noise can't
// false-positive. Age is measured from the file's mtime: any later edit
// refreshes it, which is correct — an edited file that still matches is
// still exposed.
func Exposure(e *project.Environment, projectName string, now time.Time) (ExposureReport, error) {
	rep := ExposureReport{Ref: projectName + "@" + e.Name, Path: e.Path}
	st, err := os.Stat(e.Path)
	if err != nil {
		if os.IsNotExist(err) {
			rep.Missing = true
			return rep, nil
		}
		return rep, err
	}
	diskVars, err := project.ReadEnvFile(e.Path)
	if err != nil {
		return rep, err
	}
	disk := make(map[string]string, len(diskVars))
	for _, dv := range diskVars {
		disk[dv.Key] = dv.Value.Reveal()
	}
	for _, vv := range e.Vars {
		real := vv.Value.Reveal()
		if len(real) < MinExposedLen {
			continue
		}
		if disk[vv.Key] == real {
			rep.HotKeys = append(rep.HotKeys, vv.Key)
		}
	}
	rep.Age = now.Sub(st.ModTime())
	return rep, nil
}
