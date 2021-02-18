package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/jetstack/cert-manager/test/acme/dns"
)

var (
	zone = os.Getenv("TEST_ZONE_NAME")
	kubeBuilderBinPath = "./kubebuilder/bin"
)

func TestRunsSuite(t *testing.T) {
	// The manifest path should contain a file named config.json that is a
	// snippet of valid configuration that should be included on the
	// ChallengeRequest passed as part of the test cases.

	fixture := dns.NewFixture(&dodeDNSProviderSolver{},
		dns.SetResolvedFQDN(fmt.Sprintf("_acme-challenge.%s",zone)),
		dns.SetResolvedZone(zone),
		dns.SetBinariesPath(kubeBuilderBinPath),
		dns.SetAllowAmbientCredentials(false),
		dns.SetManifestPath("testdata/my-custom-solver"),
	)

	fixture.RunConformance(t)
}
