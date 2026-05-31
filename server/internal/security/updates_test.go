package security

import "testing"

func TestCompareImageDigest(t *testing.T) {
	status := CompareImageDigest("alpine:3.20", "sha256:a", "sha256:b")
	if !status.Known || !status.UpdateAvailable {
		t.Fatalf("digest status = %#v", status)
	}
	unknown := CompareImageDigest("alpine:3.20", "", "sha256:b")
	if unknown.Known || unknown.UpdateAvailable {
		t.Fatalf("unknown digest status = %#v", unknown)
	}
}

func TestParseAptUpdates(t *testing.T) {
	raw := "podman/noble-updates 5.0 amd64 [upgradable from: 4.9]\nnottracked/noble 1 amd64 [upgradable from: 0]\n"
	updates := ParseAptUpdates(raw, TrackedPodmanPackages())
	if len(updates) != 1 || updates[0].Name != "podman" || !updates[0].UpdateAvailable {
		t.Fatalf("apt updates = %#v", updates)
	}
}

func TestParseDnfUpdates(t *testing.T) {
	raw := "podman.x86_64 5.0 updates\nconmon.x86_64 2.1 updates\n"
	updates := ParseDnfUpdates(raw, TrackedPodmanPackages())
	if len(updates) != 2 || updates[0].Name != "podman" || updates[1].Name != "conmon" {
		t.Fatalf("dnf updates = %#v", updates)
	}
}
