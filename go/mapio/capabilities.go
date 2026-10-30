package mapio

import (
	"runtime/debug"

	"github.com/auto-pigeon/auto-pigeon-libraries/go/mapio/apmap"
)

// ModulePath is the Go module this package ships in.
const ModulePath = "github.com/auto-pigeon/auto-pigeon-libraries"

// Capabilities is what this build of the library can do: facts a wrapper reports so a supervisor can
// check them rather than assume them from a version number.
type Capabilities struct {
	// APMapCurrent is the version written; APMapReadable every version read.
	APMapCurrent  string
	APMapReadable []string
	// ContractSource says where the contract came from.
	ContractSource string
	// Directions is the conversion matrix.
	Directions []Direction
}

// CapabilitiesOf describes a contract's capabilities. Nil means the embedded contract.
func CapabilitiesOf(contract *apmap.Contract) (Capabilities, error) {
	if contract == nil {
		embedded, err := apmap.EmbeddedContract()
		if err != nil {
			return Capabilities{}, err
		}
		contract = embedded
	}
	return Capabilities{
		APMapCurrent:   contract.Version,
		APMapReadable:  contract.ReadableVersions(),
		ContractSource: contract.Source,
		Directions:     Directions(),
	}, nil
}

// BuildModuleVersion is the version of this module compiled into the running binary, as the
// consumer's go.mod pinned it, or "(devel)" for a binary built inside this repository or against a
// replaced checkout. It is how a binary names the exact source its converter came from.
func BuildModuleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(devel)"
	}
	if info.Main.Path == ModulePath {
		return info.Main.Version
	}
	for _, dependency := range info.Deps {
		if dependency.Path == ModulePath {
			return dependency.Version
		}
	}
	return "(devel)"
}
