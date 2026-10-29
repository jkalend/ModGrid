// Package localfs owns the supported local-filesystem profile: bounded portable
// regular-file trees, exclusive staging, OS locks and checked publication.
package localfs

import (
	"modgrid.local/modgrid/internal/document"
	"path/filepath"
	"runtime"
	"strings"
)

const MaxFileBytes int64 = 268435456
const MaxTreeBytes int64 = 2147483648
const MaxEntries = 10000

func Path(name string) (string, error) {
	return pathPolicy(name, false)
}

// TreePath is the installed-runtime tree policy. Official Maven/Fabric paths
// contain '+'. Archive-import PortablePathV1 remains the stricter Path policy.
func TreePath(name string) (string, error) { return pathPolicy(name, true) }

func pathPolicy(name string, native bool) (string, error) {
	parts := strings.Split(name, "/")
	if name == "" || len(parts) > 32 {
		return "", document.Fail("UnsafePath", "empty path or path depth exceeds 32")
	}
	for _, p := range parts {
		base := strings.ToUpper(strings.Split(p, ".")[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" ||
			(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') ||
			strings.HasPrefix(base, "COM¹") || strings.HasPrefix(base, "COM²") || strings.HasPrefix(base, "COM³") || strings.HasPrefix(base, "LPT¹") || strings.HasPrefix(base, "LPT²") || strings.HasPrefix(base, "LPT³") {
			return "", document.Fail("ReservedDevice", "reserved device name")
		}
		if p == "" || p == "." || p == ".." || len(p) > 120 || strings.HasSuffix(p, ".") {
			return "", document.Fail("UnsafePath", "invalid path component")
		}
		for _, c := range p {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' || (native && c == '+')) {
				return "", document.Fail("UnsupportedPathCharacter", "PortablePathV1 accepts ASCII letters, digits, dot, underscore and hyphen")
			}
		}
	}
	max := 2048
	if runtime.GOOS == "windows" && !native {
		max = 180
	}
	if len(name) > max {
		return "", document.Fail("ResourceLimit", "relative path exceeds platform budget")
	}
	return strings.ToLower(name), nil
}

func TargetPath(root, name string) error {
	if _, err := TreePath(name); err != nil {
		return err
	}
	full, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		return err
	}
	max := 3072
	if len(full) > max {
		return document.Fail("ResourceLimit", "fully qualified target path exceeds platform budget")
	}
	return nil
}
