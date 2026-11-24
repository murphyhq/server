package messaging

import (
	"fmt"
	"strconv"
)

const (
	TopologyVersion    = 1
	TopologyVersionKey = "murphy-topology-version"
)

type Topology map[string]map[string][]string

func CheckTopologyVersion(resource, version string) error {
	if version == "" {
		return fmt.Errorf("%s has no %s", resource, TopologyVersionKey)
	}
	n, err := strconv.Atoi(version)
	if err != nil {
		return fmt.Errorf("%s has an invalid %s %q", resource, TopologyVersionKey, version)
	}
	if n < TopologyVersion {
		return fmt.Errorf("%s is at topology version %d, and this release needs %d", resource, n, TopologyVersion)
	}
	return nil
}
