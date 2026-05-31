package security

import "strings"

type DigestStatus struct {
	ImageName       string `json:"image_name"`
	LocalDigest     string `json:"local_digest"`
	RemoteDigest    string `json:"remote_digest"`
	UpdateAvailable bool   `json:"update_available"`
	Known           bool   `json:"known"`
}

type HostPackageUpdate struct {
	Name             string `json:"name"`
	InstalledVersion string `json:"installed_version"`
	AvailableVersion string `json:"available_version"`
	UpdateAvailable  bool   `json:"update_available"`
}

func CompareImageDigest(imageName string, localDigest string, remoteDigest string) DigestStatus {
	local := strings.TrimSpace(localDigest)
	remote := strings.TrimSpace(remoteDigest)
	return DigestStatus{
		ImageName:       imageName,
		LocalDigest:     local,
		RemoteDigest:    remote,
		UpdateAvailable: local != "" && remote != "" && local != remote,
		Known:           local != "" && remote != "",
	}
}

func ParseAptUpdates(raw string, tracked map[string]bool) []HostPackageUpdate {
	var updates []HostPackageUpdate
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.Split(fields[0], "/")[0]
		if len(tracked) > 0 && !tracked[name] {
			continue
		}
		update := HostPackageUpdate{Name: name, AvailableVersion: fields[1], UpdateAvailable: true}
		if len(fields) >= 6 && strings.Trim(fields[4], "[]") == "upgradable" {
			update.InstalledVersion = strings.Trim(fields[5], "]")
		}
		updates = append(updates, update)
	}
	return updates
}

func ParseDnfUpdates(raw string, tracked map[string]bool) []HostPackageUpdate {
	var updates []HostPackageUpdate
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.HasPrefix(fields[0], "Last") || strings.HasPrefix(fields[0], "Available") {
			continue
		}
		name := fields[0]
		if idx := strings.Index(name, "."); idx > 0 {
			name = name[:idx]
		}
		if len(tracked) > 0 && !tracked[name] {
			continue
		}
		updates = append(updates, HostPackageUpdate{Name: name, AvailableVersion: fields[1], UpdateAvailable: true})
	}
	return updates
}

func TrackedPodmanPackages() map[string]bool {
	return map[string]bool{
		"podman":         true,
		"conmon":         true,
		"crun":           true,
		"runc":           true,
		"slirp4netns":    true,
		"fuse-overlayfs": true,
		"uidmap":         true,
	}
}
