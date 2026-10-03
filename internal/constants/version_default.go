// Copyright (c) 2026 Proton AG
//
// This file is part of Proton Mail Bridge.Bridge.
//
// Proton Mail Bridge is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Proton Mail Bridge is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Proton Mail Bridge. If not, see <https://www.gnu.org/licenses/>.

//go:build !build_qa

package constants

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

//nolint:gochecknoglobals
var (
	versionCache      string
	versionCacheMutex sync.Mutex
)

// getNewestAppVersion reads the latest Android Mail release used by this fork's API headers.
func getNewestAppVersion() (string, error) {
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://github.com/ProtonMail/android-mail/releases/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get latest version: %s", resp.Status)
	}

	finalURL := resp.Request.URL.Path
	re := regexp.MustCompile(`tag\/v?(\d+\.\d+\.\d+)`)
	matches := re.FindStringSubmatch(finalURL)
	if len(matches) < 2 {
		return "", errors.New("no version found in redirect URL")
	}
	newestVersion := matches[1]
	logrus.WithField("newestVersion", newestVersion).Debug("Newest app version")
	return newestVersion, nil
}

// AppVersion returns the full rendered version of the app (to be used in request headers).
func AppVersion(version string) string {
	versionCacheMutex.Lock()
	defer versionCacheMutex.Unlock()

	if versionCache == "" {
		newestVersion, err := getNewestAppVersion()
		if err != nil {
			logrus.WithError(err).Warn("Failed to get newest app version")
			// Cache the fallback so an unavailable network cannot delay every API request.
			versionCache = version
		} else {
			versionCache = newestVersion
		}
	}
	logrus.WithField("version", versionCache).Debug("App version")
	return fmt.Sprintf("%v@%v", AppName, versionCache)
}
